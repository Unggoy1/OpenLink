//go:build windows

package hostctl

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// ManagedSession owns an authenticated bridge and the loader helper that keeps
// the original target process handle. Closing it requests native Stop; it does
// not terminate the target. The DLL remains pinned until that process exits.
type ManagedSession struct {
	Bridge      *Bridge
	command     *exec.Cmd
	input       io.WriteCloser
	done        chan struct{}
	result      error
	once        sync.Once
	closeErr    error
	diagnostics *bytes.Buffer
}

func (s *ManagedSession) Close() error {
	s.once.Do(func() {
		if s.Bridge != nil {
			s.Bridge.Close()
		}
		s.input.Close()
		select {
		case <-s.done:
			if s.result != nil {
				s.closeErr = fmt.Errorf("loader cleanup: %w: %s", s.result, s.diagnostics.String())
			}
		case <-time.After(15 * time.Second):
			s.command.Process.Kill()
			select {
			case <-s.done:
			case <-time.After(time.Second):
			}
			s.closeErr = errors.New("loader cleanup timed out; native cleanup unconfirmed")
		}
	})
	return s.closeErr
}

// StartManaged is for a process launched by the caller, with its exact exec
// path. It must never be used to adopt a pre-existing game. The separate native
// bool opts into the build-pinned backend; false is transport only.
func StartManaged(ctx context.Context, process *os.Process, executable, dll string, native bool) (_ *ManagedSession, returnError error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if process == nil || process.Pid <= 0 || executable == "" || dll == "" {
		return nil, errors.New("managed process, executable and DLL required")
	}
	dll, err := filepath.Abs(dll)
	if err != nil {
		return nil, err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return nil, err
	}
	helper := filepath.Join(filepath.Dir(dll), "hostctl-loader.exe")
	for _, path := range []string{dll, executable, helper} {
		info, e := os.Stat(path)
		if e != nil {
			return nil, e
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("regular file required: %s", path)
		}
	}
	var token [32]byte
	if _, err = rand.Read(token[:]); err != nil {
		return nil, err
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, err
	}
	defer listener.Close()
	startup, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	stopListener := context.AfterFunc(startup, func() { listener.Close() })
	defer stopListener()
	cfg := make([]byte, 48)
	defer clear(cfg)
	binary.LittleEndian.PutUint32(cfg, 48)
	version := uint32(1)
	if native {
		version = 2
	}
	binary.LittleEndian.PutUint32(cfg[4:], version)
	binary.LittleEndian.PutUint16(cfg[8:], uint16(listener.Addr().(*net.TCPAddr).Port))
	binary.LittleEndian.PutUint32(cfg[12:], uint32(os.Getpid()))
	copy(cfg[16:], token[:])
	// Duplicate the original os.Process launch handle, never OpenProcess(PID).
	current, err := syscall.GetCurrentProcess()
	if err != nil {
		return nil, err
	}
	var inherited syscall.Handle
	var duplicateError error
	err = process.WithHandle(func(original uintptr) {
		duplicateError = syscall.DuplicateHandle(current, syscall.Handle(original), current, &inherited, 0, true, syscall.DUPLICATE_SAME_ACCESS)
	})
	if err != nil {
		return nil, err
	}
	if duplicateError != nil {
		return nil, duplicateError
	}
	defer syscall.CloseHandle(inherited)
	command := exec.Command(helper, strconv.FormatUint(uint64(inherited), 10), strconv.Itoa(process.Pid), dll, executable)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000, AdditionalInheritedHandles: []syscall.Handle{inherited}}
	input, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := command.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, err
	}
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	if err = command.Start(); err != nil {
		input.Close()
		output.Close()
		return nil, err
	}
	session := &ManagedSession{command: command, input: input, done: make(chan struct{}), diagnostics: &diagnostics}
	go func() { session.result = command.Wait(); close(session.done) }()
	success := false
	defer func() {
		if !success {
			if cleanupError := session.Close(); cleanupError != nil {
				returnError = errors.Join(returnError, cleanupError)
			}
		}
	}()
	if _, err = input.Write(cfg); err != nil {
		return nil, fmt.Errorf("loader config: %w", err)
	}
	clear(cfg)
	ready := make(chan error, 1)
	go func() {
		var b [4]byte
		_, e := io.ReadFull(output, b[:])
		if e == nil && binary.LittleEndian.Uint32(b[:]) != 0 {
			e = fmt.Errorf("loader rejected target/start: Windows error %d", binary.LittleEndian.Uint32(b[:]))
		}
		ready <- e
	}()
	select {
	case err = <-ready:
		if err != nil {
			return nil, err
		}
	case <-startup.Done():
		return nil, startup.Err()
	}
	connection, err := listener.AcceptTCP()
	if err != nil {
		return nil, fmt.Errorf("DLL connection: %w", err)
	}
	session.Bridge, err = AcceptBridge(startup, connection, token, uint32(process.Pid))
	if err != nil {
		return nil, err
	}
	success = true
	return session, nil
}
