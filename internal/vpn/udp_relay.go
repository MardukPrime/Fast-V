package vpn

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
)

// udpRelay forwards datagrams for one tun flow through a SOCKS5 UDP
// association. Each association is tied to a single flow (client + destination)
// so replies are unambiguous without a shared NAT table.
type udpRelay struct {
	ctrl      net.Conn
	sock      *net.UDPConn
	addr      netip.AddrPort
	mu        sync.Mutex
	buf       []byte
	closeOnce sync.Once
}

func newUDPRelay(ctx context.Context, dialer socksDialer) (*udpRelay, error) {
	ctrl, relayAddr, err := dialer.associate(ctx)
	if err != nil {
		return nil, err
	}
	sock, err := net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(relayAddr))
	if err != nil {
		_ = ctrl.Close()
		return nil, fmt.Errorf("dial socks udp relay %s: %w", relayAddr, err)
	}
	return &udpRelay{ctrl: ctrl, sock: sock, addr: relayAddr}, nil
}

// send encapsulates one datagram for dst.
func (r *udpRelay) send(payload []byte, dst netip.AddrPort) error {
	header := make([]byte, 0, 22)
	header = append(header, 0, 0, 0) // RSV, RSV, FRAG=0 (no fragmentation)
	header = appendAddress(header, dst)
	packet := make([]byte, 0, len(header)+len(payload))
	packet = append(packet, header...)
	packet = append(packet, payload...)

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.sock.Write(packet); err != nil {
		return fmt.Errorf("socks udp send: %w", err)
	}
	return nil
}

// receive returns the next datagram and its source address.
func (r *udpRelay) receive() ([]byte, netip.AddrPort, error) {
	if cap(r.buf) < 64*1024 {
		r.buf = make([]byte, 64*1024)
	}
	n, err := r.sock.Read(r.buf)
	if err != nil {
		return nil, netip.AddrPort{}, err
	}
	if n < 4 {
		return nil, netip.AddrPort{}, errors.New("socks udp reply: short header")
	}
	atyp := r.buf[3]
	offset := 4
	var addr netip.Addr
	switch atyp {
	case socksAtypIPv4:
		if n < offset+4+2 {
			return nil, netip.AddrPort{}, errors.New("socks udp reply: short ipv4 header")
		}
		addr = netip.AddrFrom4([4]byte(r.buf[offset : offset+4]))
		offset += 4
	case socksAtypV6:
		if n < offset+16+2 {
			return nil, netip.AddrPort{}, errors.New("socks udp reply: short ipv6 header")
		}
		var raw [16]byte
		copy(raw[:], r.buf[offset:offset+16])
		addr = netip.AddrFrom16(raw)
		offset += 16
	default:
		return nil, netip.AddrPort{}, errors.New("socks udp reply: unsupported address type")
	}
	if n < offset+2 {
		return nil, netip.AddrPort{}, errors.New("socks udp reply: short port")
	}
	port := binary.BigEndian.Uint16(r.buf[offset : offset+2])
	offset += 2
	return r.buf[offset:n], netip.AddrPortFrom(addr, port), nil
}

// Close ends the association.
func (r *udpRelay) Close() error {
	r.closeOnce.Do(func() {
		_ = r.sock.Close()
		_ = r.ctrl.Close()
	})
	return nil
}
