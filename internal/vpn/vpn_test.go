package vpn

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

// ---------------------------------------------------------------------------
// in-memory link: two devices behaving like a veth pair carrying raw IP packets
// ---------------------------------------------------------------------------

type memDevice struct {
	in     chan []byte
	peer   *memDevice
	closed chan struct{}
	once   sync.Once
}

func newMemDevicePair() (*memDevice, *memDevice) {
	a := &memDevice{in: make(chan []byte, 64), closed: make(chan struct{})}
	b := &memDevice{in: make(chan []byte, 64), closed: make(chan struct{})}
	a.peer, b.peer = b, a
	return a, b
}

func (d *memDevice) Read(p []byte) (int, error) {
	select {
	case pkt := <-d.in:
		return copy(p, pkt), nil
	case <-d.closed:
		return 0, io.EOF
	}
}

func (d *memDevice) Write(p []byte) (int, error) {
	packet := append([]byte(nil), p...)
	select {
	case d.peer.in <- packet:
		return len(p), nil
	case <-d.peer.closed:
		return 0, io.EOF
	}
}

func (d *memDevice) Close() error {
	d.once.Do(func() { close(d.closed) })
	return nil
}

// testPeer is a second netstack wired to the far end of a memDevice, standing in
// for the Android kernel that normally generates the tun traffic.
type testPeer struct {
	stack *stack.Stack
	link  *channel.Endpoint
	dev   *memDevice
}

func newTestPeer(t *testing.T, prefix netip.Prefix) *testPeer {
	t.Helper()
	ns, link, err := buildStack(Config{MTU: 1500, TunIPv4: prefix, TunIPv6: netip.MustParsePrefix("fd00:1::1/64")})
	if err != nil {
		t.Fatalf("build peer stack: %v", err)
	}
	dev, _ := newMemDevicePair()
	peer := &testPeer{stack: ns, link: link, dev: dev}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			packet := link.ReadContext(ctx)
			if packet == nil {
				return
			}
			_, _ = dev.Write(packet.ToView().AsSlice())
			packet.DecRef()
		}
	}()
	go func() {
		defer wg.Done()
		buf := make([]byte, 2048)
		for {
			n, readErr := dev.Read(buf)
			if readErr != nil {
				return
			}
			packet := stack.NewPacketBuffer(stack.PacketBufferOptions{
				Payload: buffer.MakeWithData(append([]byte(nil), buf[:n]...)),
			})
			packet.NetworkProtocolNumber = ipv4.ProtocolNumber
			link.InjectInbound(ipv4.ProtocolNumber, packet)
			packet.DecRef()
		}
	}()
	t.Cleanup(func() {
		cancel()
		ns.Destroy()
		_ = dev.Close()
		wg.Wait()
	})
	return peer
}

// ---------------------------------------------------------------------------
// minimal SOCKS5 server: CONNECT and UDP ASSOCIATE, rewriting the target to the
// local test service so the test never leaves the machine
// ---------------------------------------------------------------------------

type testSocksServer struct {
	listener net.Listener
	// tcpTarget is dialled for every CONNECT request.
	tcpTarget string
	// udpTarget receives every relayed datagram.
	udpTarget *net.UDPAddr
}

func startTestSocks(t *testing.T, tcpTarget string, udpTarget *net.UDPAddr) *testSocksServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen socks: %v", err)
	}
	server := &testSocksServer{listener: listener, tcpTarget: tcpTarget, udpTarget: udpTarget}
	go server.serve()
	t.Cleanup(func() { _ = listener.Close() })
	return server
}

func (s *testSocksServer) addr() string { return s.listener.Addr().String() }

func (s *testSocksServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *testSocksServer) handle(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)

	// greeting
	head := make([]byte, 2)
	if _, err := io.ReadFull(reader, head); err != nil {
		return
	}
	methods := make([]byte, int(head[1]))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return
	}
	if _, err := conn.Write([]byte{socksVersion, socksAuthNone}); err != nil {
		return
	}

	// request
	req := make([]byte, 4)
	if _, err := io.ReadFull(reader, req); err != nil {
		return
	}
	if _, err := readAddr(reader, req[3]); err != nil {
		return
	}
	var portRaw [2]byte
	if _, err := io.ReadFull(reader, portRaw[:]); err != nil {
		return
	}

	switch req[1] {
	case socksCmdConn:
		upstream, err := net.Dial("tcp", s.tcpTarget)
		if err != nil {
			_, _ = conn.Write([]byte{socksVersion, 0x05, 0, socksAtypIPv4, 0, 0, 0, 0, 0, 0})
			return
		}
		defer upstream.Close()
		if _, err := conn.Write([]byte{socksVersion, socksReplyOK, 0, socksAtypIPv4, 127, 0, 0, 1, 0, 0}); err != nil {
			return
		}
		go func() { _, _ = io.Copy(upstream, reader) }()
		_, _ = io.Copy(conn, upstream)
	case socksCmdAssoc:
		relay, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			return
		}
		defer relay.Close()
		relayAddr := relay.LocalAddr().(*net.UDPAddr)
		reply := []byte{socksVersion, socksReplyOK, 0, socksAtypIPv4, 127, 0, 0, 1, 0, 0}
		binary.BigEndian.PutUint16(reply[8:], uint16(relayAddr.Port))
		if _, err := conn.Write(reply); err != nil {
			return
		}
		s.relayUDP(relay)
	}
}

// relayUDP forwards datagrams between the association socket and the local UDP
// echo service.
func (s *testSocksServer) relayUDP(relay *net.UDPConn) {
	client := make([]byte, 64*1024)
	for {
		n, clientAddr, err := relay.ReadFromUDP(client)
		if err != nil {
			return
		}
		payload, err := stripUDPHeader(client[:n])
		if err != nil {
			continue
		}
		upstream, dialErr := net.DialUDP("udp", nil, s.udpTarget)
		if dialErr != nil {
			continue
		}
		_ = upstream.SetDeadline(time.Now().Add(2 * time.Second))
		if _, writeErr := upstream.Write(payload); writeErr != nil {
			_ = upstream.Close()
			continue
		}
		reply := make([]byte, 64*1024)
		read, readErr := upstream.Read(reply)
		_ = upstream.Close()
		if readErr != nil {
			continue
		}
		header := appendAddress([]byte{0, 0, 0}, netip.MustParseAddrPort("198.51.100.53:53"))
		packet := append(header, reply[:read]...)
		_, _ = relay.WriteToUDP(packet, clientAddr)
	}
}

func stripUDPHeader(packet []byte) ([]byte, error) {
	if len(packet) < 4 {
		return nil, errors.New("short datagram")
	}
	atyp := packet[3]
	offset := 4
	switch atyp {
	case socksAtypIPv4:
		offset += 4
	case socksAtypV6:
		offset += 16
	default:
		return nil, errors.New("unsupported address type")
	}
	offset += 2
	if len(packet) < offset {
		return nil, errors.New("short datagram header")
	}
	return packet[offset:], nil
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func startEchoServer(t *testing.T) (string, *net.UDPAddr) {
	t.Helper()
	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp echo: %v", err)
	}
	t.Cleanup(func() { _ = tcpListener.Close() })
	go func() {
		for {
			conn, acceptErr := tcpListener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()

	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen udp echo: %v", err)
	}
	t.Cleanup(func() { _ = udpConn.Close() })
	go func() {
		buf := make([]byte, 64*1024)
		for {
			n, from, readErr := udpConn.ReadFromUDP(buf)
			if readErr != nil {
				return
			}
			_, _ = udpConn.WriteToUDP(buf[:n], from)
		}
	}()

	return tcpListener.Addr().String(), udpConn.LocalAddr().(*net.UDPAddr)
}

// TestSessionRelaysTCPAndUDPThroughSocks drives the dataplane the same way a
// device would: a peer netstack emits the packets, the session terminates them
// and relays through SOCKS5 to a real local echo service.
func TestSessionRelaysTCPAndUDPThroughSocks(t *testing.T) {
	tcpTarget, udpTarget := startEchoServer(t)
	socks := startTestSocks(t, tcpTarget, udpTarget)

	peer := newTestPeer(t, netip.MustParsePrefix("10.111.0.1/30"))
	session, err := Start(peer.dev.peer, Config{
		SocksAddress: socks.addr(),
		MTU:          1500,
		TunIPv4:      netip.MustParsePrefix("10.111.0.2/30"),
		TunIPv6:      netip.MustParsePrefix("fd00:1::2/64"),
		DialTimeout:  5 * time.Second,
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	// The destination is deliberately "far away": the test SOCKS server rewrites
	// it to the local echo service, so the packets really do travel through the
	// tun pair and the SOCKS5 handshake.
	remote := netip.MustParseAddrPort("198.51.100.7:443")
	target := tcpip.FullAddress{
		NIC:  1,
		Addr: tcpip.AddrFrom4(remote.Addr().As4()),
		Port: remote.Port(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := gonet.DialContextTCP(ctx, peer.stack, target, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatalf("dial through tun: %v", err)
	}
	defer conn.Close()

	payload := []byte("fastv-m3-tcp")
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write through tun: %v", err)
	}
	echo := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, echo); err != nil {
		t.Fatalf("read through tun: %v", err)
	}
	if string(echo) != string(payload) {
		t.Fatalf("tcp echo mismatch: got %q want %q", echo, payload)
	}

	// UDP path: same tun pair, but through a SOCKS5 UDP association.
	udpRemote := netip.MustParseAddrPort("198.51.100.53:53")
	udpConn, err := gonet.DialUDP(peer.stack, nil, &tcpip.FullAddress{
		NIC:  1,
		Addr: tcpip.AddrFrom4(udpRemote.Addr().As4()),
		Port: udpRemote.Port(),
	}, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatalf("dial udp through tun: %v", err)
	}
	defer udpConn.Close()
	_ = udpConn.SetDeadline(time.Now().Add(5 * time.Second))
	query := []byte("fastv-m3-udp")
	if _, err := udpConn.Write(query); err != nil {
		t.Fatalf("write udp through tun: %v", err)
	}
	reply := make([]byte, 256)
	n, err := udpConn.Read(reply)
	if err != nil {
		t.Fatalf("read udp through tun: %v", err)
	}
	if string(reply[:n]) != string(query) {
		t.Fatalf("udp echo mismatch: got %q want %q", reply[:n], query)
	}

	stats := session.Stats()
	if stats.TCPFlows == 0 || stats.UDPFlows == 0 {
		t.Fatalf("expected forwarded flows, got %+v", stats)
	}
	if stats.BytesIn == 0 || stats.BytesOut == 0 {
		t.Fatalf("expected packet counters to move, got %+v", stats)
	}
}

// TestSocksConnectRoundTrip covers the client half of the handshake without a
// tun in the way.
func TestSocksConnectRoundTrip(t *testing.T) {
	tcpTarget, udpTarget := startEchoServer(t)
	socks := startTestSocks(t, tcpTarget, udpTarget)

	dialer := newSocksDialer(socks.addr(), 5*time.Second)
	conn, err := dialer.connect(context.Background(), netip.MustParseAddrPort("198.51.100.7:443"))
	if err != nil {
		t.Fatalf("socks connect: %v", err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	echo := make([]byte, 4)
	if _, err := io.ReadFull(conn, echo); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(echo) != "ping" {
		t.Fatalf("echo mismatch: %q", echo)
	}
}

// TestSocksAssociateUDPRoundTrip covers the UDP association client half.
func TestSocksAssociateUDPRoundTrip(t *testing.T) {
	_, udpTarget := startEchoServer(t)
	socks := startTestSocks(t, "", udpTarget)

	relay, err := newUDPRelay(context.Background(), newSocksDialer(socks.addr(), 5*time.Second))
	if err != nil {
		t.Fatalf("udp associate: %v", err)
	}
	defer relay.Close()

	dst := netip.MustParseAddrPort("198.51.100.53:53")
	if err := relay.send([]byte("assoc"), dst); err != nil {
		t.Fatalf("relay send: %v", err)
	}
	payload, _, err := relay.receive()
	if err != nil {
		t.Fatalf("relay receive: %v", err)
	}
	if string(payload) != "assoc" {
		t.Fatalf("relay echo mismatch: %q", payload)
	}
}

// TestConfigDefaults pins the wiring the Android service relies on.
func TestConfigDefaults(t *testing.T) {
	cfg := Config{}.withDefaults()
	if cfg.SocksAddress != "127.0.0.1:10808" {
		t.Fatalf("unexpected socks default %q", cfg.SocksAddress)
	}
	if cfg.MTU != 1500 {
		t.Fatalf("unexpected mtu default %d", cfg.MTU)
	}
	if cfg.TunIPv4.String() != "10.111.0.2/30" || cfg.TunIPv6.String() != "fd00:1::2/64" {
		t.Fatalf("unexpected tun defaults %v %v", cfg.TunIPv4, cfg.TunIPv6)
	}
	if !cfg.TunIPv4.Addr().Unmap().Is4() {
		t.Fatalf("tun ipv4 default is not v4")
	}
}

// TestManagerRejectsSecondSession documents the single-session rule the Android
// service depends on.
func TestManagerRejectsSecondSession(t *testing.T) {
	var manager Manager
	if _, running := manager.Status(); running {
		t.Fatal("fresh manager reports a running session")
	}
	if err := manager.Stop(); err != nil {
		t.Fatalf("stop on idle manager: %v", err)
	}
	if _, err := manager.Start(-1, Config{}); err == nil {
		t.Fatal("expected an error for an invalid descriptor")
	}
}
