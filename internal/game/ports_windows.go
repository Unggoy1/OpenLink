package game

import (
	"encoding/binary"
	"fmt"
	"net"
	"syscall"
	"unsafe"
)

var procGetExtendedUdpTable = syscall.NewLazyDLL("iphlpapi.dll").NewProc("GetExtendedUdpTable")

// UDPPortOwners returns the sockets bound to a local UDP port (IPv4 and IPv6)
// with their owning PIDs. A test bind is not a reliable "in use" check: the
// game's sockets allow sharing, so a second bind can succeed.
func UDPPortOwners(port int) ([]Owner, error) {
	var out []Owner
	for _, af := range []uint32{syscall.AF_INET, syscall.AF_INET6} {
		o, err := udpOwners(af, port)
		if err != nil {
			return nil, err
		}
		out = append(out, o...)
	}
	return out, nil
}

func udpOwners(af uint32, port int) ([]Owner, error) {
	const udpTableOwnerPID = 1
	var size uint32
	buf := make([]byte, 64<<10)
	for i := 0; i < 4; i++ {
		size = uint32(len(buf))
		r, _, _ := procGetExtendedUdpTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)),
			0, uintptr(af), udpTableOwnerPID, 0)
		if r == 0 {
			break
		}
		if r != uintptr(syscall.ERROR_INSUFFICIENT_BUFFER) {
			return nil, fmt.Errorf("GetExtendedUdpTable: error %d", r)
		}
		buf = make([]byte, size+4096)
		if i == 3 {
			return nil, fmt.Errorf("GetExtendedUdpTable: table keeps growing")
		}
	}
	// MIB_UDPTABLE_OWNER_PID: DWORD count, then rows of {addr, port, pid}
	// (12 bytes). MIB_UDP6TABLE_OWNER_PID rows: {addr[16], scope, port, pid} (28 bytes).
	rowSize, addrLen, portOff, pidOff := 12, 4, 4, 8
	if af == syscall.AF_INET6 {
		rowSize, addrLen, portOff, pidOff = 28, 16, 20, 24
	}
	n := int(binary.LittleEndian.Uint32(buf))
	var owners []Owner
	for i := 0; i < n; i++ {
		row := buf[4+i*rowSize:]
		if len(row) < rowSize {
			break
		}
		// The port is stored in network byte order in the low 16 bits.
		p := int(binary.BigEndian.Uint16(row[portOff : portOff+2]))
		if p == port {
			ip := make(net.IP, addrLen)
			copy(ip, row[:addrLen])
			owners = append(owners, Owner{PID: int(binary.LittleEndian.Uint32(row[pidOff : pidOff+4])), IP: ip})
		}
	}
	return owners, nil
}
