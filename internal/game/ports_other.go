//go:build !windows

package game

import "errors"

// UDPPortOwners is only implemented on Windows, where the game runs.
func UDPPortOwners(port int) ([]Owner, error) {
	return nil, errors.New("UDPPortOwners: not supported on this OS")
}
