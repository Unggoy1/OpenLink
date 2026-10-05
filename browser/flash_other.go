//go:build !windows

package main

// flashWindow is Windows-only; elsewhere the notification alone alerts the player.
func flashWindow(string) {}

// playChime is Windows-only.
func playChime() {}
