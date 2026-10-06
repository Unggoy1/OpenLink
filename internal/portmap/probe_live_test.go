package portmap

import (
	"context"
	"encoding/xml"
	"os"
	"strings"
	"testing"
	"time"
)

// Read-only check against the real router; runs only with PORTMAP_LIVE=1.
// It never adds or removes a mapping.
func TestLiveDiscoveryReadOnly(t *testing.T) {
	if os.Getenv("PORTMAP_LIVE") == "" {
		t.Skip("set PORTMAP_LIVE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ip, err := LocalIPv4()
	if err != nil {
		t.Fatal(err)
	}
	gw, gerr := defaultGateway()
	if gerr != nil {
		t.Fatal(gerr)
	}
	loc, err := discoverGateway(ctx, ip, gw)
	if err != nil {
		t.Logf("UPnP: %v", err)
		return
	}
	control, service, err := findWANService(ctx, loc)
	if err != nil {
		t.Logf("UPnP description: %v", err)
		return
	}
	t.Logf("UPnP gateway found; service %s", service)
	body, err := soap(ctx, control, service, "GetExternalIPAddress", nil)
	if err != nil {
		t.Logf("GetExternalIPAddress: %v", err)
		return
	}
	var v struct {
		IP string `xml:"Body>GetExternalIPAddressResponse>NewExternalIPAddress"`
	}
	xml.Unmarshal(body, &v)
	t.Logf("router reports an external address (shared/CGNAT: %v, empty: %v)", SharedAddress(parseIP(v.IP)), strings.TrimSpace(v.IP) == "")
}
