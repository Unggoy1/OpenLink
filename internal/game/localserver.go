package game

import (
	"os"
	"path/filepath"
	"strings"
)

// LocalServerRunning reports whether a Halo LAN server, or an OpenLink Server
// fronting one, runs on this PC: such a process holds UDP 1343, which a game
// client never binds (clients start at 1353, O015). Our own process is
// skipped. False when it cannot tell (other OSes, unreadable processes).
func LocalServerRunning() bool {
	owners, err := UDPPortOwners(1343)
	if err != nil {
		return false
	}
	for _, o := range owners {
		if o.PID == os.Getpid() {
			continue
		}
		switch strings.ToLower(filepath.Base(processImage(o.PID))) {
		case "haloinfinite.exe", "openlink-server.exe":
			return true
		}
	}
	return false
}
