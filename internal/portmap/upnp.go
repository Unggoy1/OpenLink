package portmap

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ssdpAddr is the UPnP discovery multicast address; a variable for tests.
var ssdpAddr = "239.255.255.250:1900"

// upnpClient talks only to the gateway: no redirects to other hosts.
var upnpClient = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// wanService is the service type of a gateway's WAN connection; it is also
// put into SOAP requests, so nothing else is accepted.
var wanService = regexp.MustCompile(`^urn:schemas-upnp-org:service:WAN(IP|PPP)Connection:[0-9]+$`)

// discoverGateway finds the UPnP internet gateway at gw (the default gateway)
// from localIP and returns its description URL. Answers from other devices,
// and descriptions hosted anywhere but on gw, are ignored: any LAN device can
// answer discovery.
func discoverGateway(ctx context.Context, localIP, gw net.IP) (string, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: localIP})
	if err != nil {
		return "", err
	}
	defer conn.Close()
	dst, err := net.ResolveUDPAddr("udp4", ssdpAddr)
	if err != nil {
		return "", err
	}
	for _, st := range []string{"urn:schemas-upnp-org:device:InternetGatewayDevice:1", "urn:schemas-upnp-org:device:InternetGatewayDevice:2"} {
		msg := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 2\r\nST: " + st + "\r\n\r\n"
		if _, err := conn.WriteToUDP([]byte(msg), dst); err != nil {
			return "", err
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetReadDeadline(deadline)
	buf := make([]byte, 2048)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			return "", errors.New("no UPnP gateway answered (UPnP may be off in the router)")
		}
		if !from.IP.Equal(gw) {
			continue
		}
		resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(buf[:n])), nil)
		if err != nil {
			continue
		}
		loc := resp.Header.Get("Location")
		resp.Body.Close()
		if u, err := url.Parse(loc); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() == gw.String() {
			return loc, nil
		}
	}
}

type upnpDevice struct {
	Services []struct {
		Type       string `xml:"serviceType"`
		ControlURL string `xml:"controlURL"`
	} `xml:"serviceList>service"`
	Devices []upnpDevice `xml:"deviceList>device"`
}

// findWANService returns the control URL and service type of the gateway's
// WANIPConnection (or WANPPPConnection) service.
func findWANService(ctx context.Context, location string) (control, service string, err error) {
	body, err := httpGet(ctx, location)
	if err != nil {
		return "", "", err
	}
	var root struct {
		URLBase string     `xml:"URLBase"`
		Device  upnpDevice `xml:"device"`
	}
	if err := xml.Unmarshal(body, &root); err != nil {
		return "", "", fmt.Errorf("gateway description: %w", err)
	}
	var walk func(d upnpDevice) bool
	walk = func(d upnpDevice) bool {
		for _, s := range d.Services {
			if wanService.MatchString(strings.TrimSpace(s.Type)) {
				control, service = strings.TrimSpace(s.ControlURL), strings.TrimSpace(s.Type)
				return true
			}
		}
		for _, c := range d.Devices {
			if walk(c) {
				return true
			}
		}
		return false
	}
	if !walk(root.Device) {
		return "", "", errors.New("the gateway has no WAN connection service")
	}
	base := location
	if root.URLBase != "" {
		base = root.URLBase
	}
	b, err := url.Parse(base)
	if err != nil {
		return "", "", err
	}
	c, err := b.Parse(control)
	if err != nil {
		return "", "", err
	}
	// The control endpoint must be on the gateway itself, where the
	// description came from.
	if l, err := url.Parse(location); err != nil || c.Hostname() != l.Hostname() || (c.Scheme != "http" && c.Scheme != "https") {
		return "", "", errors.New("the gateway's control URL points to another host")
	}
	return c.String(), service, nil
}

func httpGet(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := upnpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", u, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// soap calls one action and returns the response body. A UPnP fault comes
// back as an error with its errorCode.
func soap(ctx context.Context, control, service, action string, args [][2]string) ([]byte, error) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	fmt.Fprintf(&b, `<u:%s xmlns:u="%s">`, action, service)
	for _, a := range args {
		fmt.Fprintf(&b, "<%s>", a[0])
		xml.EscapeText(&b, []byte(a[1]))
		fmt.Fprintf(&b, "</%s>", a[0])
	}
	fmt.Fprintf(&b, "</u:%s></s:Body></s:Envelope>", action)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, control, strings.NewReader(b.String()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header[textproto.CanonicalMIMEHeaderKey("SOAPAction")] = []string{`"` + service + "#" + action + `"`}
	resp, err := upnpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		var fault struct {
			Code int    `xml:"Body>Fault>detail>UPnPError>errorCode"`
			Desc string `xml:"Body>Fault>detail>UPnPError>errorDescription"`
		}
		xml.Unmarshal(body, &fault)
		return nil, &upnpError{Action: action, Code: fault.Code, Desc: fault.Desc, Status: resp.Status}
	}
	return body, nil
}

type upnpError struct {
	Action, Desc, Status string
	Code                 int
}

func (e *upnpError) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("%s refused: %d %s", e.Action, e.Code, e.Desc)
	}
	return fmt.Sprintf("%s refused: %s", e.Action, e.Status)
}

type upnpMapping struct {
	control, service string
	r                Request
	lease            time.Duration
	external         string
}

func (m *upnpMapping) Method() string       { return "UPnP" }
func (m *upnpMapping) ExternalIP() string   { return m.external }
func (m *upnpMapping) Lease() time.Duration { return m.lease }

func (m *upnpMapping) add(ctx context.Context, lease time.Duration) error {
	_, err := soap(ctx, m.control, m.service, "AddPortMapping", [][2]string{
		{"NewRemoteHost", ""}, {"NewExternalPort", strconv.Itoa(m.r.ExternalPort)}, {"NewProtocol", "UDP"},
		{"NewInternalPort", strconv.Itoa(m.r.InternalPort)}, {"NewInternalClient", m.r.InternalIP.String()},
		{"NewEnabled", "1"}, {"NewPortMappingDescription", m.r.Description},
		{"NewLeaseDuration", strconv.Itoa(int(lease / time.Second))},
	})
	return err
}

// Renew re-adds the mapping (routers treat a repeat as a refresh).
func (m *upnpMapping) Renew(ctx context.Context) error { return m.add(ctx, m.lease) }

func (m *upnpMapping) Remove(ctx context.Context) error {
	_, err := soap(ctx, m.control, m.service, "DeletePortMapping", [][2]string{
		{"NewRemoteHost", ""}, {"NewExternalPort", strconv.Itoa(m.r.ExternalPort)}, {"NewProtocol", "UDP"},
	})
	return err
}

func mapUPnP(ctx context.Context, r Request) (Mapping, error) {
	gw, err := findGateway()
	if err != nil {
		return nil, fmt.Errorf("default gateway: %w", err)
	}
	loc, err := discoverGateway(ctx, r.InternalIP, gw.To4())
	if err != nil {
		return nil, err
	}
	control, service, err := findWANService(ctx, loc)
	if err != nil {
		return nil, err
	}
	m := &upnpMapping{control: control, service: service, r: r, lease: r.Lease}
	err = m.add(ctx, m.lease)
	var ue *upnpError
	if errors.As(err, &ue) && ue.Code == 725 { // OnlyPermanentLeasesSupported
		m.lease = 0
		err = m.add(ctx, 0)
	}
	if err != nil {
		return nil, err
	}
	if body, err := soap(ctx, control, service, "GetExternalIPAddress", nil); err == nil {
		var v struct {
			IP string `xml:"Body>GetExternalIPAddressResponse>NewExternalIPAddress"`
		}
		if xml.Unmarshal(body, &v) == nil {
			m.external = strings.TrimSpace(v.IP)
		}
	}
	return m, nil
}
