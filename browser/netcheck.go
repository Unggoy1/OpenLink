package main

import (
	"net"
	"net/url"
	"strings"
)

// localNetworkURL reports whether u points at this PC or the local network:
// localhost, or a loopback, private or link-local address. Only such
// directories may use plain http (local testing); anything on the internet
// needs https, so nobody on the way can rewrite the server list.
func localNetworkURL(u string) bool {
	p, err := url.Parse(u)
	if err != nil {
		return false
	}
	return localHost(p.Hostname())
}

func localHost(h string) bool {
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}

// serverHostAllowed reports whether a listing's host may be contacted. A
// directory on the internet must not send the app to addresses on the
// player's own network (it could use the app to probe them); a local test
// directory may.
func serverHostAllowed(directory, host string) bool {
	if localNetworkURL(directory) {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return true // a DNS name: the directory only lists names for trusted hosts
	}
	return !(localHost(host) || ip.IsUnspecified() || ip.IsMulticast())
}
