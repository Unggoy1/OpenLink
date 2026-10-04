//go:build !windows

package game

import "os/exec"

func setNewConsole(*exec.Cmd) {}
