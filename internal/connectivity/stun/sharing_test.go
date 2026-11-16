package stun

import (
	"bytes"
	"net/netip"
	"testing"
	"time"
)

func sharedServer(t *testing.T, mode Mode) *Server {
	t.Helper()
	cfg := Config{PublicIP: server, Port: 3074, AltPort: 3075, Mode: mode, RelayLo: 20000, RelayHi: 20199, RelayMaxClients: 2, DirectWindow: time.Second}
	s := &Server{Config: cfg, Log: discard, DiscoveryVersion: 2}
	if err := s.AddDiscovery(1, discard); err != nil {
		t.Fatal(err)
	}
	s.router = NewRouter(cfg, discard, func(id int) (netip.AddrPort, error) { return netip.AddrPortFrom(server, uint16(20000+id)), nil }, func(int) {})
	s.initDiscovery()
	return s
}
func TestSharedDiscoveryKeepsNATPoliciesAndRelayCapacitySeparate(t *testing.T) {
	s := sharedServer(t, Adaptive)
	// More OutRun clients than the WWE relay capacity must not allocate any WWE
	// endpoint, or change its adaptive NAT behavior.
	for i := 1; i <= 10; i++ {
		client := netip.AddrPortFrom(netip.AddrFrom4([4]byte{198, 51, 100, byte(i)}), 3074)
		out := s.routeDiscovery(unhex("1ea50100"), client, primary, t0)
		if len(out) != 1 || len(out[0].Payload) != 12 || unpackAddr(out[0].Payload[4:10]) != client {
			t.Fatal("v1 discovery failed")
		}
		out = s.routeDiscovery(unhex("1400010002000000"), client, primary, at(0.1))
		if len(out) != 1 || !out[0].From.Alt {
			t.Fatal("v1 inherited owner's adaptive NAT policy")
		}
		s.routeDiscovery(keepalivePacket, client, primary, at(0.2))
		if _, exists := s.router.peers[client]; exists {
			t.Fatal("v1 traversal leaked to the owner")
		}
	}
	if len(s.router.relay.clients) != 0 {
		t.Fatal("v1 consumed WWE relay capacity")
	}
	wwe := netip.MustParseAddrPort("203.0.113.20:40000")
	if out := s.routeDiscovery(unhex("1e0200"), wwe, primary, at(0.3)); len(out) != 1 || len(out[0].Payload) != 9 {
		t.Fatal("v2 discovery failed")
	}
	if len(s.router.relay.clients) != 1 {
		t.Fatal("v2 lost adaptive registration")
	}
	out := s.routeDiscovery(unhex("14020002"), wwe, primary, at(0.4))
	if len(out) != 1 || out[0].From.Alt {
		t.Fatal("WWE adaptive NAT policy changed")
	}
}
func TestSharedTraversalCannotCrossGamePeerNamespaces(t *testing.T) {
	s := sharedServer(t, Direct)
	a := netip.MustParseAddrPort("198.51.100.1:3074")
	b := netip.MustParseAddrPort("198.51.100.2:3074")
	s.routeDiscovery(unhex("1e0200"), a, primary, t0)
	s.routeDiscovery(unhex("1e000100"), b, primary, t0)
	for _, pair := range [][2]netip.AddrPort{{a, b}, {b, a}} {
		packet := traversalPacket(introRequest, 7, pair[0], pair[1], [10]byte{})
		if out := s.routeDiscovery(packet, pair[0], primary, at(1)); len(out) != 0 {
			t.Fatal("cross-game introduction forwarded")
		}
	}
	// Switching games on a reused NAT endpoint changes discovery ownership.
	s.routeDiscovery(unhex("1e000100"), a, primary, at(2))
	if _, ok := s.router.peers[a]; ok {
		t.Fatal("old discovery ownership retained")
	}
	out := s.routeDiscovery(traversalPacket(introRequest, 8, a, b, [10]byte{}), a, primary, at(3))
	if len(out) != 1 || out[0].To != b {
		t.Fatal("same-game traversal failed after switching")
	}
}

func TestSharedDiscoveryRejectsUnsupportedAndDuplicateVersions(t *testing.T) {
	s := &Server{DiscoveryVersion: 2}
	for _, version := range []uint16{0, 3} {
		if err := s.AddDiscovery(version, discard); err == nil {
			t.Fatalf("accepted version %d", version)
		}
	}
	if err := s.AddDiscovery(1, discard); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDiscovery(1, discard); err == nil {
		t.Fatal("accepted duplicate version")
	}
	if err := (&Server{}).AddDiscovery(1, discard); err == nil {
		t.Fatal("accepted unknown owner version")
	}
}

func TestMixedDiscoveryVersionsKeepSDKTraversalAndAdaptiveFallback(t *testing.T) {
	// Two Stacked-like v1 SDK clients behind strict NAT, plus v2 SDK and v1
	// native-discovery clients sharing the public IP. UDP cannot identify titles.
	n := newNetwork(t, Adaptive, []string{"EDM", "EDM", "EDM", "EIM"}, true)
	s := &Server{Config: n.router.cfg, Log: discard, DiscoveryVersion: 2, router: n.router}
	if err := s.AddDiscovery(1, discard); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTransport(1); err != nil {
		t.Fatal(err)
	}
	s.initDiscovery()
	if len(s.discovery) != 0 {
		t.Fatal("v1 SDK packets would be captured by the native-only router")
	}
	for i, version := range []uint16{1, 1, 2, 1} {
		control := n.clients[i].control
		out := s.routeDiscovery(header(ipRequest, version), control, primary, t0)
		offset := 3
		if version == 1 {
			offset = 4
		}
		if len(out) != 1 || !bytes.Equal(out[0].Payload, ipReplyPacket(version, control)) || unpackAddr(out[0].Payload[offset:offset+6]) != control {
			t.Fatal("mixed discovery lost its version or advertised a virtual native endpoint")
		}
		nat := append(header(natRequest, version), changePort)
		if version == 1 {
			nat = append(nat, 0, 0, 0)
		}
		out = s.routeDiscovery(nat, control, primary, at(0.01))
		if len(out) != 1 || out[0].Reason != "coordinated-nat" {
			t.Fatal("mixed discovery did not use the common NAT policy")
		}
	}
	src, dst := n.clients[0].control, n.clients[1].control
	packet := traversalPacket(introRequest, 123, src, dst, [10]byte([]byte("0123456789")))
	n.outgoing(0, dst)
	n.waiting[[2]int{0, 1}] = append([]byte{introReply}, packet[1:]...)
	n.pump(s.routeDiscovery(packet, src, primary, at(0.1)), at(0.2), true)
	if _, ok := n.resolved[[2]int{0, 1}]; ok {
		t.Fatal("strict NAT unexpectedly connected directly")
	}
	n.pump(s.routeDiscovery(packet, src, primary, at(1.3)), at(1.4), true)
	if n.resolved[[2]int{0, 1}] != n.clients[1].virtual {
		t.Fatal("v1 discovery did not reach the SDK adaptive relay")
	}
	for _, pair := range [][2]int{{0, 1}, {1, 0}} {
		n.pump(n.send(pair[0], pair[1], payload, at(1.5)), at(1.6), true)
		got := n.last(pair[1])
		if got.from != n.clients[pair[0]].virtual || !bytes.Equal(got.data, payload) {
			t.Fatal("shared transport altered or misrouted opaque gameplay")
		}
	}
	if len(n.received[2]) != 0 || len(n.received[3]) != 0 {
		t.Fatal("gameplay leaked to unrelated discovery clients")
	}
}

func TestNativeAndSDKDiscoveryOverlapRejectsVirtualPolicy(t *testing.T) {
	for _, nativeFirst := range []bool{false, true} {
		s := &Server{Config: Config{Mode: Relay}, DiscoveryVersion: 2}
		var first, second error
		if nativeFirst {
			first = s.AddDiscovery(1, discard)
			second = s.AddTransport(1)
		} else {
			first = s.AddTransport(1)
			second = s.AddDiscovery(1, discard)
		}
		if first != nil || second == nil {
			t.Fatal("ambiguous virtual-address discovery accepted")
		}
	}
	if err := (&Server{Config: Config{Mode: Relay}, DiscoveryVersion: 1}).AddDiscovery(1, discard); err == nil {
		t.Fatal("native discovery inherited its SDK owner's virtual addresses")
	}
}
