package game

import (
	"os/exec"
	"syscall"
)

// setNewConsole gives the server its own console window instead of sharing ours.
func setNewConsole(cmd *exec.Cmd) {
	const createNewConsole = 0x00000010
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewConsole}
}
