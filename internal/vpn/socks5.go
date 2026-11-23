package vpn

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"time"
)

// SOCKS5 wire constants (RFC 1928).
const (
	socksVersion   = 0x05
	socksAuthNone  = 0x00
	socksCmdBind   = 0x02
	socksCmdConn   = 0x01
	socksCmdAssoc  = 0x03
	socksAtypIPv4  = 0x01
	socksAtypV6    = 0x04
	socksReplyOK   = 0x00
	socksReplyMask = 0x07
)

// socksDialer speaks the client half of SOCKS5 against the local inbound served
// by the embedded core. Only the no-auth method is offered: the inbound listens
// on loopback inside the app sandbox.
type socksDialer struct {
	addr    string
	timeout time.Duration
}

func newSocksDialer(addr string, timeout time.Duration) socksDialer {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return socksDialer{addr: addr, timeout: timeout}
}

func (d socksDialer) dial(ctx context.Context) (net.Conn, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", d.addr)
	if err != nil {
		return nil, fmt.Errorf("dial socks %s: %w", d.addr, err)
	}
	if err := negotiate(conn, d.timeout); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// connect opens a tunnelled stream to target.
func (d socksDialer) connect(ctx context.Context, target netip.AddrPort) (net.Conn, error) {
	conn, err := d.dial(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := request(conn, d.timeout, socksCmdConn, target); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// associate opens a UDP association and returns the control connection plus the
// relay endpoint reported by the server. The control connection keeps the
// association alive and must be closed to tear it down.
func (d socksDialer) associate(ctx context.Context) (net.Conn, netip.AddrPort, error) {
	conn, err := d.dial(ctx)
	if err != nil {
		return nil, netip.AddrPort{}, err
	}
	relay, err := request(conn, d.timeout, socksCmdAssoc, netip.AddrPort{})
	if err != nil {
		_ = conn.Close()
		return nil, netip.AddrPort{}, err
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return nil, netip.AddrPort{}, err
	}
	// A server may answer 0.0.0.0 when it listens on every interface; the relay
	// is on loopback next to the inbound in that case.
	if !relay.Addr().IsValid() || relay.Addr().IsUnspecified() {
		host, _, splitErr := net.SplitHostPort(d.addr)
		if splitErr != nil {
			_ = conn.Close()
			return nil, netip.AddrPort{}, fmt.Errorf("socks relay address: %w", splitErr)
		}
		addr, parseErr := netip.ParseAddr(host)
		if parseErr != nil {
			_ = conn.Close()
			return nil, netip.AddrPort{}, fmt.Errorf("socks relay host %q: %w", host, parseErr)
		}
		relay = netip.AddrPortFrom(addr.Unmap(), relay.Port())
	}
	return conn, relay, nil
}

// negotiate performs the method-selection greeting.
func negotiate(conn net.Conn, timeout time.Duration) error {
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	if _, err := conn.Write([]byte{socksVersion, 1, socksAuthNone}); err != nil {
		return fmt.Errorf("socks greeting: %w", err)
	}
	var reply [2]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		return fmt.Errorf("socks greeting reply: %w", err)
	}
	if reply[0] != socksVersion {
		return fmt.Errorf("socks greeting reply: unexpected version %#x", reply[0])
	}
	if reply[1] != socksAuthNone {
		return fmt.Errorf("socks greeting reply: server requires method %#x", reply[1])
	}
	return nil
}

// request sends one command and parses the bound address from the reply.
func request(conn net.Conn, timeout time.Duration, cmd byte, target netip.AddrPort) (netip.AddrPort, error) {
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return netip.AddrPort{}, err
	}
	buf := make([]byte, 0, 22)
	buf = append(buf, socksVersion, cmd, 0)
	buf = appendAddress(buf, target)
	if _, err := conn.Write(buf); err != nil {
		return netip.AddrPort{}, fmt.Errorf("socks request: %w", err)
	}
	return readReply(conn)
}

func readReply(conn net.Conn) (netip.AddrPort, error) {
	var head [4]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		return netip.AddrPort{}, fmt.Errorf("socks reply: %w", err)
	}
	if head[0] != socksVersion {
		return netip.AddrPort{}, fmt.Errorf("socks reply: unexpected version %#x", head[0])
	}
	if head[1] != socksReplyOK {
		return netip.AddrPort{}, fmt.Errorf("socks reply: server returned %s", replyText(head[1]))
	}
	addr, err := readAddr(conn, head[3])
	if err != nil {
		return netip.AddrPort{}, err
	}
	var port [2]byte
	if _, err := io.ReadFull(conn, port[:]); err != nil {
		return netip.AddrPort{}, fmt.Errorf("socks reply port: %w", err)
	}
	return netip.AddrPortFrom(addr, binary.BigEndian.Uint16(port[:])), nil
}

func replyText(code byte) string {
	switch code {
	case 1:
		return "general failure (0x01)"
	case 2:
		return "connection not allowed (0x02)"
	case 3:
		return "network unreachable (0x03)"
	case 4:
		return "host unreachable (0x04)"
	case 5:
		return "connection refused (0x05)"
	case 6:
		return "TTL expired (0x06)"
	case 7:
		return "command not supported (0x07)"
	case 8:
		return "address type not supported (0x08)"
	default:
		return fmt.Sprintf("unknown error (0x%02x)", code&socksReplyMask)
	}
}

// appendAddress appends ATYP + address + port. An invalid target is encoded as
// the 0.0.0.0:0 wildcard used by UDP ASSOCIATE.
func appendAddress(buf []byte, target netip.AddrPort) []byte {
	addr := target.Addr().Unmap()
	switch {
	case !addr.IsValid():
		buf = append(buf, socksAtypIPv4)
		buf = append(buf, 0, 0, 0, 0)
		buf = append(buf, 0, 0)
	case addr.Is4():
		v4 := addr.As4()
		buf = append(buf, socksAtypIPv4)
		buf = append(buf, v4[:]...)
		var port [2]byte
		binary.BigEndian.PutUint16(port[:], target.Port())
		buf = append(buf, port[:]...)
	default:
		v6 := addr.As16()
		buf = append(buf, socksAtypV6)
		buf = append(buf, v6[:]...)
		var port [2]byte
		binary.BigEndian.PutUint16(port[:], target.Port())
		buf = append(buf, port[:]...)
	}
	return buf
}

// readAddr decodes ATYP + address, leaving the port for the caller.
func readAddr(conn io.Reader, atyp byte) (netip.Addr, error) {
	switch atyp {
	case socksAtypIPv4:
		var raw [4]byte
		if _, err := io.ReadFull(conn, raw[:]); err != nil {
			return netip.Addr{}, fmt.Errorf("socks reply ipv4: %w", err)
		}
		return netip.AddrFrom4(raw), nil
	case socksAtypV6:
		var raw [16]byte
		if _, err := io.ReadFull(conn, raw[:]); err != nil {
			return netip.Addr{}, fmt.Errorf("socks reply ipv6: %w", err)
		}
		return netip.AddrFrom16(raw), nil
	default:
		return netip.Addr{}, errors.New("socks reply: domain names are not handled on the tun path")
	}
}
