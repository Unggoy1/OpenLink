package portmap

import (
	"encoding/binary"
	"errors"
	"net"
	"syscall"
	"unsafe"
)

var procGetBestRoute = syscall.NewLazyDLL("iphlpapi.dll").NewProc("GetBestRoute")

// defaultGateway returns the next hop of the route to the internet.
func defaultGateway() (net.IP, error) {
	var row [56]byte // MIB_IPFORWARDROW
	dest := binary.LittleEndian.Uint32(net.IPv4(8, 8, 8, 8).To4())
	if r, _, _ := procGetBestRoute.Call(uintptr(dest), 0, uintptr(unsafe.Pointer(&row[0]))); r != 0 {
		return nil, syscall.Errno(r)
	}
	hop := net.IP(append([]byte(nil), row[12:16]...)) // dwForwardNextHop, network order
	if hop.IsUnspecified() {
		return nil, errors.New("no gateway on the default route")
	}
	return hop, nil
}
