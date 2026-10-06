package portmap

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

var loopback = net.IPv4(127, 0, 0, 1)

// fakeIGD is a UPnP gateway: SSDP answers plus description and SOAP over HTTP.
type fakeIGD struct {
	mu        sync.Mutex
	actions   []string
	permanent bool // refuse non-zero leases with 725
	ssdp      *net.UDPConn
	http      *httptest.Server
}

func newFakeIGD(t *testing.T) *fakeIGD {
	f := &fakeIGD{}
	f.http = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.http.Close)
	var err error
	if f.ssdp, err = net.ListenUDP("udp4", &net.UDPAddr{IP: loopback}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.ssdp.Close() })
	go func() {
		buf := make([]byte, 1024)
		for {
			n, from, err := f.ssdp.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if strings.Contains(string(buf[:n]), "InternetGatewayDevice:1") {
				f.ssdp.WriteToUDP([]byte("HTTP/1.1 200 OK\r\nST: x\r\nLOCATION: "+f.http.URL+"/desc.xml\r\n\r\n"), from)
			}
		}
	}()
	old := ssdpAddr
	ssdpAddr = f.ssdp.LocalAddr().String()
	t.Cleanup(func() { ssdpAddr = old })
	return f
}

func (f *fakeIGD) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		fmt.Fprint(w, `<root><device><deviceList><device><deviceList><device><serviceList><service>
<serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType><controlURL>/ctl/IPConn</controlURL>
</service></serviceList></device></deviceList></device></deviceList></device></root>`)
		return
	}
	body, _ := io.ReadAll(r.Body)
	action := strings.TrimSuffix(strings.SplitN(r.Header.Get("SOAPAction"), "#", 2)[1], `"`)
	f.mu.Lock()
	f.actions = append(f.actions, action)
	permanent := f.permanent
	f.mu.Unlock()
	if r.URL.Path != "/ctl/IPConn" {
		http.NotFound(w, r)
		return
	}
	switch {
	case action == "AddPortMapping" && permanent && !strings.Contains(string(body), "<NewLeaseDuration>0<"):
		w.WriteHeader(500)
		fmt.Fprint(w, `<s:Envelope><s:Body><s:Fault><detail><UPnPError><errorCode>725</errorCode><errorDescription>OnlyPermanentLeasesSupported</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`)
	case action == "AddPortMapping" && !strings.Contains(string(body), "<NewInternalClient>127.0.0.1<"):
		w.WriteHeader(500)
	case action == "GetExternalIPAddress":
		fmt.Fprint(w, `<s:Envelope><s:Body><u:GetExternalIPAddressResponse><NewExternalIPAddress>203.0.113.5</NewExternalIPAddress></u:GetExternalIPAddressResponse></s:Body></s:Envelope>`)
	default:
		fmt.Fprint(w, `<s:Envelope><s:Body/></s:Envelope>`)
	}
}

func req() Request {
	return Request{ExternalPort: 1343, InternalPort: 1343, InternalIP: loopback, Description: "OpenLink Server", Lease: time.Hour}
}

func TestUPnP(t *testing.T) {
	for _, permanent := range []bool{false, true} {
		f := newFakeIGD(t)
		f.permanent = permanent
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		m, err := Map(ctx, req())
		if err != nil {
			t.Fatal(err)
		}
		if m.Method() != "UPnP" || m.ExternalIP() != "203.0.113.5" || (m.Lease() == 0) != permanent {
			t.Fatalf("mapping %s %s %v", m.Method(), m.ExternalIP(), m.Lease())
		}
		if m.Renew(ctx) != nil || m.Remove(ctx) != nil {
			t.Fatal("renew/remove")
		}
		f.mu.Lock()
		got := strings.Join(f.actions, ",")
		f.mu.Unlock()
		want := "AddPortMapping,GetExternalIPAddress,AddPortMapping,DeletePortMapping"
		if permanent {
			want = "AddPortMapping," + want
		}
		if got != want {
			t.Fatalf("permanent=%v actions %s", permanent, got)
		}
	}
}

// fakeNATPMP answers NAT-PMP on loopback, granting offer as the external port.
func fakeNATPMP(t *testing.T, offer uint16) *[][]byte {
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: loopback})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	oldPort, oldGW, oldSSDP := natpmpPort, findGateway, ssdpAddr
	natpmpPort = c.LocalAddr().(*net.UDPAddr).Port
	findGateway = func() (net.IP, error) { return loopback, nil }
	ssdpAddr = "127.0.0.1:9" // nothing answers UPnP
	t.Cleanup(func() { natpmpPort, findGateway, ssdpAddr = oldPort, oldGW, oldSSDP })
	var seen [][]byte
	go func() {
		buf := make([]byte, 64)
		for {
			n, from, err := c.ReadFromUDP(buf)
			if err != nil {
				return
			}
			seen = append(seen, append([]byte(nil), buf[:n]...))
			if buf[1] == 0 {
				c.WriteToUDP([]byte{0, 128, 0, 0, 0, 0, 0, 1, 198, 51, 100, 7}, from)
				continue
			}
			resp := make([]byte, 16)
			resp[1] = 129
			copy(resp[8:10], buf[4:6])
			ext := offer
			if binary.BigEndian.Uint32(buf[8:]) == 0 {
				ext = 0
			}
			binary.BigEndian.PutUint16(resp[10:], ext)
			copy(resp[12:16], buf[8:12])
			c.WriteToUDP(resp, from)
		}
	}()
	return &seen
}

func TestNATPMP(t *testing.T) {
	seen := fakeNATPMP(t, 1343)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m, err := Map(ctx, req())
	if err != nil {
		t.Fatal(err)
	}
	if m.Method() != "NAT-PMP" || m.ExternalIP() != "198.51.100.7" || m.Lease() != time.Hour {
		t.Fatalf("mapping %s %s %v", m.Method(), m.ExternalIP(), m.Lease())
	}
	if err := m.Remove(ctx); err != nil {
		t.Fatal(err)
	}
	last := (*seen)[len(*seen)-1]
	if last[1] != 1 || binary.BigEndian.Uint32(last[8:]) != 0 {
		t.Fatalf("remove request %x", last)
	}
}

func TestNATPMPOtherPortIsRefused(t *testing.T) {
	fakeNATPMP(t, 40000)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := Map(ctx, req()); err == nil || !strings.Contains(err.Error(), "offered port 40000") {
		t.Fatalf("err %v", err)
	}
}

func TestSharedAddress(t *testing.T) {
	for ip, want := range map[string]bool{"100.72.1.2": true, "192.168.1.1": true, "10.0.0.1": true, "203.0.113.5": false, "8.8.8.8": false} {
		if SharedAddress(net.ParseIP(ip)) != want {
			t.Errorf("SharedAddress(%s) != %v", ip, want)
		}
	}
}
