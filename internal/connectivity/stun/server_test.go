package stun

import (
	"bytes"
	"context"
	"net"
	"net/netip"
	"testing"
	"time"
)

func freePort(t *testing.T) int {
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).Port
}

// console answers relay probes like the game and returns other datagrams.
func console(t *testing.T, c *net.UDPConn) (netip.AddrPort, []byte) {
	buf := make([]byte, 2048)
	for {
		c.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, from, err := c.ReadFromUDPAddrPort(buf)
		if err != nil {
			t.Fatal(err)
		}
		if buf[0] == introForward && n == traversalSize {
			echo := append([]byte{introReply}, buf[1:n]...)
			c.WriteToUDPAddrPort(echo, unpackAddr(buf[17:23]))
			continue
		}
		return from, bytes.Clone(buf[:n])
	}
}

func answerProbes(c *net.UDPConn) {
	buf := make([]byte, 2048)
	for {
		n, _, err := c.ReadFromUDPAddrPort(buf)
		if err != nil {
			return
		}
		if buf[0] == introForward && n == traversalSize {
			c.WriteToUDPAddrPort(append([]byte{introReply}, buf[1:n]...), unpackAddr(buf[17:23]))
		}
	}
}

func TestServerRelaysOverUDP(t *testing.T) {
	lo := freePort(t)
	s := &Server{Config: Config{PublicIP: netip.MustParseAddr("127.0.0.1"), Port: freePort(t), AltPort: freePort(t),
		Mode: Relay, RelayLo: lo, RelayHi: lo + 3, RelayMaxClients: 2}, Bind: "127.0.0.1", Log: discard, DiscoveryVersion: 2}
	if err := s.AddTransport(1); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error)
	go func() { done <- s.Run(ctx) }()
	time.Sleep(100 * time.Millisecond)

	stun := netip.AddrPortFrom(s.PublicIP, uint16(s.Port))
	var conns [2]*net.UDPConn
	var virtual [2]netip.AddrPort
	for i := range conns {
		c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		conns[i] = c
		// One padded-v1 and one packed-v2 client use the same actual sockets
		// and allocator. The reply must retain each client's discovery format.
		version := uint16(i + 1)
		c.WriteToUDPAddrPort(header(ipRequest, version), stun)
		_, reply := console(t, c)
		offset := 3
		if version == 1 {
			offset = 4
		}
		if len(reply) != len(ipReplyPacket(version, stun)) {
			t.Fatal("wrong shared discovery reply format")
		}
		virtual[i] = unpackAddr(reply[offset : offset+6])
		if virtual[i].Port() < uint16(lo) || virtual[i].Port() > uint16(lo+3) {
			t.Fatalf("advertised %v outside the relay range", virtual[i])
		}
	}

	conns[0].WriteToUDPAddrPort(payload, virtual[1])
	go answerProbes(conns[0]) // client 0 must prove its mapping toward 1's endpoint
	from, got := console(t, conns[1])
	if from != virtual[0] || !bytes.Equal(got, payload) {
		t.Fatalf("client 1 got %x from %v", got, from)
	}

	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatal(err)
	}
}
