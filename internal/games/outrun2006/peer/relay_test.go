package peer

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestRelayAdmissionAndNativePackets(t *testing.T) {
	r := New(netip.MustParseAddrPort("127.0.0.1:3658"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Now()
	a, b := Token{1}, Token{2}
	// Same public IP, different native mappings: no IP-only identity assumptions.
	ea := netip.MustParseAddrPort("192.0.2.1:40000")
	eb := netip.MustParseAddrPort("192.0.2.1:40001")
	reg := func(token Token, ep netip.AddrPort) netip.Addr {
		t.Helper()
		dst, ack := r.Handle(ep, Frame(Register, token, netip.Addr{}, nil), now)
		if dst != ep || len(ack) != HeaderSize {
			t.Fatal("registration failed")
		}
		return netip.AddrFrom4([4]byte(ack[24:28]))
	}
	if _, out := r.Handle(ea, Frame(Register, a, netip.Addr{}, nil), now); out != nil {
		t.Fatal("unauthenticated registration")
	}
	r.Authorize(a, ea.Addr(), "a")
	r.Authorize(b, eb.Addr(), "b")
	va, vb := reg(a, ea), reg(b, eb)
	if va == vb {
		t.Fatal("shared virtual identity")
	}
	payload := []byte{0x12, 0x34, 0x17, 0, 0xff}
	frame := Frame(Data, a, vb, payload)
	if _, out := r.Handle(ea, frame, now); out != nil {
		t.Fatal("undisclosed peer admitted")
	}
	r.Offer("room", a, b)
	dst, out := r.Handle(ea, frame, now)
	if dst != eb || !bytes.Equal(out, Frame(Deliver, b, va, payload)) {
		t.Fatal("opaque native packet not delivered")
	}
	if _, out = r.Handle(netip.MustParseAddrPort("192.0.2.2:40000"), frame, now); out != nil {
		t.Fatal("wrong public IP admitted")
	}
	changed := netip.MustParseAddrPort("192.0.2.1:40002")
	if _, out = r.Handle(changed, frame, now); out != nil {
		t.Fatal("data changed mapping")
	}
	if reg(a, changed) != va {
		t.Fatal("re-registration changed virtual address")
	}
	if _, out = r.Handle(ea, frame, now); out != nil {
		t.Fatal("old endpoint still admitted")
	}
	if dst, out = r.Handle(changed, frame, now); dst != eb || out == nil {
		t.Fatal("rebinding failed")
	}
	if _, out = r.Handle(changed, frame, now.Add(31*time.Second)); out != nil {
		t.Fatal("stale mapping admitted")
	}
	r.DropRoom("room")
	if _, out = r.Handle(changed, frame, now); out != nil {
		t.Fatal("removed room still admitted")
	}
	r.Offer("room", a, b)
	r.Revoke(b, "b")
	if _, out = r.Handle(changed, frame, now); out != nil {
		t.Fatal("logged out peer admitted")
	}
	for _, invalid := range [][]byte{nil, []byte("AOR1"), Frame(Data, a, vb, make([]byte, MaxPayload+1)), append(frame, 0)} {
		if _, out = r.Handle(changed, invalid, now); out != nil {
			t.Fatal("invalid packet admitted")
		}
	}
}
func TestRoomMeshAndConnectionOwnership(t *testing.T) {
	r := New(netip.MustParseAddrPort("127.0.0.1:3658"), nil)
	now := time.Now()
	tokens := []Token{{1}, {2}, {3}}
	eps := []netip.AddrPort{}
	ips := []netip.Addr{}
	for i, token := range tokens {
		ep := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(40000+i))
		eps = append(eps, ep)
		r.Authorize(token, ep.Addr(), i)
		_, ack := r.Handle(ep, Frame(Register, token, netip.Addr{}, nil), now)
		ips = append(ips, netip.AddrFrom4([4]byte(ack[24:28])))
	}
	r.Offer("room", tokens[0], tokens[1])
	r.Offer("room", tokens[0], tokens[2])
	if dst, _ := r.Handle(eps[1], Frame(Data, tokens[1], ips[2], []byte{0, 0, 0x17}), now); dst != eps[2] {
		t.Fatal("guests cannot exchange room mesh packets")
	}
	r.Authorize(tokens[1], eps[1].Addr(), "reconnect")
	r.Revoke(tokens[1], 1)
	if dst, _ := r.Handle(eps[1], Frame(Data, tokens[1], ips[2], []byte{0, 0, 0x17}), now); dst != eps[2] {
		t.Fatal("closing old gateway revoked replacement")
	}
}
func TestUDPLifecycle(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := New(netip.MustParseAddrPort("127.0.0.1:0"), log)
	// Exercise an actual socket without taking a fixed test port.
	probe, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(r.Bind))
	if err != nil {
		t.Fatal(err)
	}
	r.Bind = probe.LocalAddr().(*net.UDPAddr).AddrPort()
	probe.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	c, err := net.ListenUDP("udp4", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	token := Token{4}
	r.Authorize(token, netip.MustParseAddr("127.0.0.1"), "test")
	buf := make([]byte, 100)
	received := false
	for i := 0; i < 20; i++ {
		c.WriteToUDPAddrPort(Frame(Register, token, netip.Addr{}, nil), r.Bind)
		c.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
		n, _, err := c.ReadFromUDPAddrPort(buf)
		if err == nil && n == HeaderSize {
			received = true
			break
		}
	}
	if !received {
		t.Fatal("no ACK over real UDP")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("relay did not stop")
	}
}
