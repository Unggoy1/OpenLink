package game

import (
	"net"
	"os"
	"slices"
	"testing"
)

func TestUDPPortOwnersFindsOurSocket(t *testing.T) {
	for _, network := range []string{"udp4", "udp6"} {
		c, err := net.ListenUDP(network, nil)
		if err != nil {
			t.Skipf("%s unavailable: %v", network, err)
		}
		port := c.LocalAddr().(*net.UDPAddr).Port
		owners, err := UDPPortOwners(port)
		c.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(owners, func(o Owner) bool { return o.PID == os.Getpid() && o.Covers("") }) {
			t.Fatalf("%s port %d owners %v do not include us (%d)", network, port, owners, os.Getpid())
		}
	}
}
