//go:build !windows

package game

import "os/exec"

func setNewConsole(*exec.Cmd) {}

func processImage(int) string { return "" }
