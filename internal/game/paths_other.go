//go:build !windows && !linux

package game

// platformSteamRoots: no known Steam locations; pass the install folder.
func platformSteamRoots() []string { return nil }
