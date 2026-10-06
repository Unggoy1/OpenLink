package portmap

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

// natpmpPort is the NAT-PMP server port on the gateway; a variable for tests.
var natpmpPort = 5351

// findGateway returns the default gateway; a variable for tests.
var findGateway = defaultGateway

// natpmpCall sends one NAT-PMP request to the gateway and returns the reply,
// retrying with the protocol's doubling timeout (250 ms, 500 ms, 1 s).
func natpmpCall(ctx context.Context, gw, local net.IP, req []byte, op byte) ([]byte, error) {
	conn, err := net.DialUDP("udp4", &net.UDPAddr{IP: local}, &net.UDPAddr{IP: gw, Port: natpmpPort})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	buf := make([]byte, 16)
	wait := 250 * time.Millisecond
	for try := 0; try < 3 && ctx.Err() == nil; try++ {
		if _, err := conn.Write(req); err != nil {
			return nil, err
		}
		conn.SetReadDeadline(time.Now().Add(wait))
		wait *= 2
		for {
			n, err := conn.Read(buf)
			if err != nil {
				break
			}
			if n >= 4 && buf[0] == 0 && buf[1] == 128+op {
				if code := binary.BigEndian.Uint16(buf[2:]); code != 0 {
					return nil, fmt.Errorf("gateway refused (result %d)", code)
				}
				return append([]byte(nil), buf[:n]...), nil
			}
		}
	}
	return nil, errors.New("no NAT-PMP answer from the gateway")
}

type natpmpMapping struct {
	gw, local net.IP
	r         Request
	external  int
	lease     time.Duration
	extIP     string
}

func (m *natpmpMapping) Method() string       { return "NAT-PMP" }
func (m *natpmpMapping) ExternalIP() string   { return m.extIP }
func (m *natpmpMapping) Lease() time.Duration { return m.lease }

// request maps (lease > 0) or removes (lease 0) the UDP mapping.
func (m *natpmpMapping) request(ctx context.Context, lease time.Duration) error {
	req := make([]byte, 12)
	req[1] = 1 // map UDP
	binary.BigEndian.PutUint16(req[4:], uint16(m.r.InternalPort))
	if lease > 0 {
		binary.BigEndian.PutUint16(req[6:], uint16(m.r.ExternalPort))
	}
	binary.BigEndian.PutUint32(req[8:], uint32(lease/time.Second))
	resp, err := natpmpCall(ctx, m.gw, m.local, req, 1)
	if err != nil {
		return err
	}
	if len(resp) < 16 {
		return errors.New("short NAT-PMP reply")
	}
	if lease > 0 {
		m.external = int(binary.BigEndian.Uint16(resp[10:]))
		m.lease = time.Duration(binary.BigEndian.Uint32(resp[12:])) * time.Second
		if m.external != m.r.ExternalPort {
			// The router picked another port; players would not find it.
			m.request(ctx, 0)
			return fmt.Errorf("the gateway offered port %d instead of %d", m.external, m.r.ExternalPort)
		}
	}
	return nil
}

func (m *natpmpMapping) Renew(ctx context.Context) error  { return m.request(ctx, m.r.Lease) }
func (m *natpmpMapping) Remove(ctx context.Context) error { return m.request(ctx, 0) }

func mapNATPMP(ctx context.Context, r Request) (Mapping, error) {
	gw, err := findGateway()
	if err != nil {
		return nil, fmt.Errorf("default gateway: %w", err)
	}
	m := &natpmpMapping{gw: gw, local: r.InternalIP, r: r}
	if r.Lease <= 0 {
		m.r.Lease = time.Hour
	}
	if err := m.request(ctx, m.r.Lease); err != nil {
		return nil, err
	}
	if resp, err := natpmpCall(ctx, gw, r.InternalIP, []byte{0, 0}, 0); err == nil && len(resp) >= 12 {
		m.extIP = net.IP(resp[8:12]).String()
	}
	return m, nil
}
