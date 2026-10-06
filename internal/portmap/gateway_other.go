//go:build !windows

package portmap

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"strings"
)

// defaultGateway reads the default route from /proc/net/route (Linux).
func defaultGateway() (net.IP, error) {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 3 || fields[1] != "00000000" {
			continue
		}
		b, err := hex.DecodeString(fields[2])
		if err != nil || len(b) != 4 {
			continue
		}
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, binary.LittleEndian.Uint32(b))
		return ip, nil
	}
	return nil, errors.New("no default route")
}
