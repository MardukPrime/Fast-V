package vpn

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

// maxTCPForwardedFlows bounds how many half-open TCP flows the forwarder keeps
// pending at once.
const maxTCPForwardedFlows = 1024

// Config describes one tun session.
type Config struct {
	// SocksAddress is the loopback SOCKS5 inbound owned by the embedded core,
	// for example 127.0.0.1:10808.
	SocksAddress string
	// MTU is the tun interface MTU.
	MTU int
	// TunIPv4 / TunIPv6 are the addresses configured on the tun interface. They
	// must match what the VpnService.Builder used, because the netstack answers
	// on exactly these addresses.
	TunIPv4 netip.Prefix
	TunIPv6 netip.Prefix
	// DialTimeout bounds the SOCKS handshake for a new flow.
	DialTimeout time.Duration
}

func (c Config) withDefaults() Config {
	if c.SocksAddress == "" {
		c.SocksAddress = "127.0.0.1:10808"
	}
	if c.MTU <= 0 || c.MTU > 65535 {
		c.MTU = 1500
	}
	if !c.TunIPv4.IsValid() || !c.TunIPv4.Addr().Is4() {
		c.TunIPv4 = netip.MustParsePrefix("10.111.0.2/30")
	}
	if !c.TunIPv6.IsValid() || !c.TunIPv6.Addr().Is6() {
		c.TunIPv6 = netip.MustParsePrefix("fd00:1::2/64")
	}
	if c.DialTimeout <= 0 {
		c.DialTimeout = 10 * time.Second
	}
	return c
}

// Stats is a point-in-time view of a session's activity.
type Stats struct {
	TCPFlows    uint64
	UDPFlows    uint64
	BytesIn     uint64
	BytesOut    uint64
	Dropped     uint64
	OpenStreams int
}

// Session is a running tun dataplane: it reads IP packets from the device,
// terminates them in userspace and relays every flow through SOCKS5.
type Session struct {
	dev     io.ReadWriter
	cfg     Config
	stack   *stack.Stack
	link    *channel.Endpoint
	socks   socksDialer
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	streams *streamRegistry

	tcpFlows atomic.Uint64
	udpFlows atomic.Uint64
	bytesIn  atomic.Uint64
	bytesOut atomic.Uint64
	dropped  atomic.Uint64
}

// Start builds the netstack for dev and begins pumping packets.
//
// dev must yield raw IP packets (as an Android VpnService tun file descriptor
// does) and is closed by Close when it implements io.Closer.
func Start(dev io.ReadWriter, cfg Config) (*Session, error) {
	cfg = cfg.withDefaults()
	ns, link, err := buildStack(cfg)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	session := &Session{
		dev:     dev,
		cfg:     cfg,
		stack:   ns,
		link:    link,
		socks:   newSocksDialer(cfg.SocksAddress, cfg.DialTimeout),
		cancel:  cancel,
		streams: newStreamRegistry(),
	}

	tcpForwarder := tcp.NewForwarder(ns, 0, maxTCPForwardedFlows, session.handleTCP)
	ns.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpForwarder.HandlePacket)
	udpForwarder := udp.NewForwarder(ns, session.handleUDP)
	ns.SetTransportProtocolHandler(udp.ProtocolNumber, udpForwarder.HandlePacket)

	session.wg.Add(2)
	go session.pumpInbound(ctx)
	go session.pumpOutbound(ctx)
	return session, nil
}

// Stats snapshots the counters of the session.
func (s *Session) Stats() Stats {
	return Stats{
		TCPFlows:    s.tcpFlows.Load(),
		UDPFlows:    s.udpFlows.Load(),
		BytesIn:     s.bytesIn.Load(),
		BytesOut:    s.bytesOut.Load(),
		Dropped:     s.dropped.Load(),
		OpenStreams: s.streams.len(),
	}
}

// Close stops the dataplane, closes every relayed flow and releases the device.
func (s *Session) Close() error {
	s.cancel()
	s.streams.closeAll()
	s.stack.Destroy()
	var err error
	if closer, ok := s.dev.(io.Closer); ok {
		err = closer.Close()
	}
	s.wg.Wait()
	return err
}

// pumpInbound moves packets from the tun device into the netstack.
func (s *Session) pumpInbound(ctx context.Context) {
	defer s.wg.Done()
	buf := make([]byte, s.cfg.MTU+128)
	for {
		if ctx.Err() != nil {
			return
		}
		n, err := s.dev.Read(buf)
		if err != nil {
			if ctx.Err() == nil && !errors.Is(err, io.EOF) {
				s.dropped.Add(1)
			}
			return
		}
		if n == 0 {
			continue
		}
		s.bytesIn.Add(uint64(n))
		if !s.inject(buf[:n]) {
			s.dropped.Add(1)
		}
	}
}

// pumpOutbound moves packets produced by the netstack back to the tun device.
func (s *Session) pumpOutbound(ctx context.Context) {
	defer s.wg.Done()
	for {
		packet := s.link.ReadContext(ctx)
		if packet == nil {
			return
		}
		payload := packet.ToView().AsSlice()
		written, err := s.dev.Write(payload)
		packet.DecRef()
		if err != nil {
			if ctx.Err() == nil {
				s.dropped.Add(1)
			}
			return
		}
		s.bytesOut.Add(uint64(written))
	}
}

// inject hands one raw IP packet to the netstack.
func (s *Session) inject(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	var proto tcpip.NetworkProtocolNumber
	switch raw[0] >> 4 {
	case 4:
		proto = ipv4.ProtocolNumber
	case 6:
		proto = ipv6.ProtocolNumber
	default:
		return false
	}
	packet := stack.NewPacketBuffer(stack.PacketBufferOptions{
		Payload: buffer.MakeWithData(append([]byte(nil), raw...)),
	})
	packet.NetworkProtocolNumber = proto
	s.link.InjectInbound(proto, packet)
	packet.DecRef()
	return true
}

// handleTCP accepts one tunnelled TCP flow and relays it through SOCKS5.
func (s *Session) handleTCP(request *tcp.ForwarderRequest) {
	id := request.ID()
	target := netip.AddrPortFrom(toNetip(id.LocalAddress), id.LocalPort)

	var queue waiter.Queue
	endpoint, endpointErr := request.CreateEndpoint(&queue)
	if endpointErr != nil {
		request.Complete(true)
		return
	}
	request.Complete(false)

	conn := gonet.NewTCPConn(&queue, endpoint)
	s.tcpFlows.Add(1)
	go func() {
		if !s.streams.add(conn) {
			_ = conn.Close()
			return
		}
		defer s.streams.remove(conn)
		defer conn.Close()
		s.relayTCP(conn, target)
	}()
}

// relayTCP bridges one netstack endpoint and one SOCKS5 stream.
func (s *Session) relayTCP(client *gonet.TCPConn, target netip.AddrPort) {
	upstream, err := s.socks.connect(context.Background(), target)
	if err != nil {
		return
	}
	if !s.streams.add(upstream) {
		_ = upstream.Close()
		return
	}
	defer s.streams.remove(upstream)
	defer upstream.Close()

	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			_ = client.Close()
			_ = upstream.Close()
		})
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(upstream, client)
		if tcpConn, ok := upstream.(*net.TCPConn); ok {
			_ = tcpConn.CloseWrite()
		}
		stop()
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(client, upstream)
		_ = client.CloseWrite()
		stop()
	}()
	wg.Wait()
}

// handleUDP accepts one tunnelled UDP flow and relays it through SOCKS5.
func (s *Session) handleUDP(request *udp.ForwarderRequest) bool {
	id := request.ID()
	target := netip.AddrPortFrom(toNetip(id.LocalAddress), id.LocalPort)

	var queue waiter.Queue
	endpoint, endpointErr := request.CreateEndpoint(&queue)
	if endpointErr != nil {
		return false
	}
	conn := gonet.NewUDPConn(&queue, endpoint)
	s.udpFlows.Add(1)
	go func() {
		if !s.streams.add(conn) {
			_ = conn.Close()
			return
		}
		defer s.streams.remove(conn)
		defer conn.Close()
		s.relayUDP(conn, target)
	}()
	return true
}

// relayUDP bridges one netstack UDP endpoint and one SOCKS5 UDP association.
func (s *Session) relayUDP(conn *gonet.UDPConn, target netip.AddrPort) {
	relay, err := newUDPRelay(context.Background(), s.socks)
	if err != nil {
		return
	}
	if !s.streams.add(relay) {
		_ = relay.Close()
		return
	}
	defer s.streams.remove(relay)
	defer relay.Close()

	done := make(chan struct{}, 2)
	go func() {
		buf := make([]byte, s.cfg.MTU)
		for {
			n, readErr := conn.Read(buf)
			if readErr != nil {
				done <- struct{}{}
				return
			}
			if sendErr := relay.send(buf[:n], target); sendErr != nil {
				s.dropped.Add(1)
			}
		}
	}()
	go func() {
		for {
			payload, _, recvErr := relay.receive()
			if recvErr != nil {
				done <- struct{}{}
				return
			}
			if _, writeErr := conn.Write(payload); writeErr != nil {
				done <- struct{}{}
				return
			}
		}
	}()
	<-done
}
