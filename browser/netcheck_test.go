package main

import "testing"

func TestDirectoryAndHostChecks(t *testing.T) {
	for u, want := range map[string]bool{
		"http://127.0.0.1:8080": true, "http://localhost:8090": true, "http://192.168.11.72:8090": true,
		"http://openlink-dir.unggoy.xyz": false, "http://203.0.113.5": false, "https://openlink-dir.unggoy.xyz": false,
	} {
		if got := localNetworkURL(u); got != want {
			t.Errorf("localNetworkURL(%s) = %v", u, got)
		}
	}
	const public, lan = "https://openlink-dir.unggoy.xyz", "http://192.168.11.72:8090"
	for _, tc := range []struct {
		dir, host string
		want      bool
	}{
		{public, "203.0.113.5", true}, {public, "host.example.net", true},
		{public, "192.168.1.10", false}, {public, "127.0.0.1", false}, {public, "0.0.0.0", false}, {public, "224.0.0.1", false},
		{lan, "192.168.11.72", true},
	} {
		if got := serverHostAllowed(tc.dir, tc.host); got != tc.want {
			t.Errorf("serverHostAllowed(%s, %s) = %v", tc.dir, tc.host, got)
		}
	}
}
