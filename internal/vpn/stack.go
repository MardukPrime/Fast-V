package vpn

import (
	"fmt"
	"net/netip"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

const nicID = tcpip.NICID(1)

// linkQueueLength bounds the number of packets queued in each direction of the
// userspace link.
const linkQueueLength = 512

// buildStack creates the netstack that terminates traffic taken from the tun
// device. The NIC runs in promiscuous mode with spoofing enabled so packets
// addressed to arbitrary internet hosts are accepted and answered with the
// original source address, exactly as a tun2socks dataplane requires.
func buildStack(cfg Config) (*stack.Stack, *channel.Endpoint, error) {
	s := stack.New(stack.Options{
		NetworkProtocols: []stack.NetworkProtocolFactory{
			ipv4.NewProtocol,
			ipv6.NewProtocol,
		},
		TransportProtocols: []stack.TransportProtocolFactory{
			tcp.NewProtocol,
			udp.NewProtocol,
		},
	})

	endpoint := channel.New(linkQueueLength, uint32(cfg.MTU), "")
	if err := s.CreateNIC(nicID, endpoint); err != nil {
		return nil, nil, fmt.Errorf("create tun nic: %v", err)
	}
	if err := s.SetPromiscuousMode(nicID, true); err != nil {
		return nil, nil, fmt.Errorf("enable promiscuous mode: %v", err)
	}
	if err := s.SetSpoofing(nicID, true); err != nil {
		return nil, nil, fmt.Errorf("enable address spoofing: %v", err)
	}

	if prefix, ok := prefixOf(cfg.TunIPv4); ok {
		addr := tcpip.AddrFrom4(prefix.Addr().As4())
		if err := s.AddProtocolAddress(nicID, tcpip.ProtocolAddress{
			Protocol:          ipv4.ProtocolNumber,
			AddressWithPrefix: tcpip.AddressWithPrefix{Address: addr, PrefixLen: prefix.Bits()},
		}, stack.AddressProperties{}); err != nil {
			return nil, nil, fmt.Errorf("add tun ipv4 address: %v", err)
		}
	}
	if prefix, ok := prefixOf(cfg.TunIPv6); ok {
		addr := tcpip.AddrFrom16(prefix.Addr().As16())
		if err := s.AddProtocolAddress(nicID, tcpip.ProtocolAddress{
			Protocol:          ipv6.ProtocolNumber,
			AddressWithPrefix: tcpip.AddressWithPrefix{Address: addr, PrefixLen: prefix.Bits()},
		}, stack.AddressProperties{}); err != nil {
			return nil, nil, fmt.Errorf("add tun ipv6 address: %v", err)
		}
	}

	routes := make([]tcpip.Route, 0, 2)
	if _, ok := prefixOf(cfg.TunIPv4); ok {
		defaultRoute, err := tcpip.NewSubnet(tcpip.AddrFrom4([4]byte{}), tcpip.MaskFromBytes(make([]byte, 4)))
		if err != nil {
			return nil, nil, fmt.Errorf("build ipv4 default route: %v", err)
		}
		routes = append(routes, tcpip.Route{Destination: defaultRoute, NIC: nicID})
	}
	if _, ok := prefixOf(cfg.TunIPv6); ok {
		defaultRoute, err := tcpip.NewSubnet(tcpip.AddrFrom16([16]byte{}), tcpip.MaskFromBytes(make([]byte, 16)))
		if err != nil {
			return nil, nil, fmt.Errorf("build ipv6 default route: %v", err)
		}
		routes = append(routes, tcpip.Route{Destination: defaultRoute, NIC: nicID})
	}
	if len(routes) == 0 {
		return nil, nil, fmt.Errorf("no tun addresses configured")
	}
	s.SetRouteTable(routes)
	return s, endpoint, nil
}

// prefixOf reports whether value can be configured on the tun NIC. The host
// bits are kept: the netstack binds the interface to the exact address the
// VpnService.Builder assigned.
func prefixOf(value netip.Prefix) (netip.Prefix, bool) {
	if !value.IsValid() || !value.Addr().IsValid() {
		return netip.Prefix{}, false
	}
	return value, true
}

// toNetip converts a netstack address into a netip value.
func toNetip(addr tcpip.Address) netip.Addr {
	parsed, ok := netip.AddrFromSlice(addr.AsSlice())
	if !ok {
		return netip.Addr{}
	}
	return parsed.Unmap()
}
