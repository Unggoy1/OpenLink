//go:build !windows

package main

import "errors"

func runAutostart(config, []string) error {
	return errors.New("autostart is only implemented on Windows; use a systemd unit or similar")
}

func autostartSummary() string { return "not available on this OS" }
