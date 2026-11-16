package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/princebyyourlogic/angelware/internal/connectivity/stun"
	"github.com/princebyyourlogic/angelware/internal/game"
	"github.com/princebyyourlogic/angelware/internal/games/familyfeud"
	"github.com/princebyyourlogic/angelware/internal/games/outrun2006"
	"github.com/princebyyourlogic/angelware/internal/games/svr2007"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

func availablePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}
func TestListenersServeDifferentDiscoveryVersionsAndShutDown(t *testing.T) {
	tcpPort := availablePort(t)
	primary, alternate := availablePort(t), availablePort(t)
	for primary == alternate {
		alternate = availablePort(t)
	}
	app, err := buildForTest(Config{Games: []GameConfig{
		{Game: svr2007.Key, Bind: "127.0.0.1", PublicIP: "127.0.0.1", TCPPort: tcpPort, STUN: Connectivity{Port: primary, AltPort: alternate}},
		{Game: outrun2006.Key, Bind: "127.0.0.2", PublicIP: "127.0.0.2", TCPPort: tcpPort, STUN: Connectivity{Port: primary, AltPort: alternate}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	defer cancel()
	// Keep one TCP client idle to verify shutdown also waits for live handlers.
	var idle net.Conn
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		idle, err = net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(tcpPort)), 20*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	for _, tt := range []struct {
		ip      string
		request []byte
		reply   int
	}{{"127.0.0.1", []byte{0x1e, 2, 0}, 9}, {"127.0.0.2", []byte{0x1e, 0xa5, 1, 0}, 12}} {
		conn, err := net.Dial("udp4", net.JoinHostPort(tt.ip, strconv.Itoa(primary)))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		var n int
		packet := make([]byte, 64)
		for time.Now().Before(deadline) {
			conn.SetDeadline(time.Now().Add(50 * time.Millisecond))
			_, err = conn.Write(tt.request)
			if err == nil {
				n, err = conn.Read(packet)
			}
			if err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err != nil || n != tt.reply || packet[0] != 0x1f {
			t.Fatalf("discovery %s: %x %v", tt.ip, packet[:n], err)
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listeners did not stop")
	}
}
func TestTCPOnlyDefinition(t *testing.T) {
	d := svr2007.Definition()
	d.STUNPort = 0
	d.AltPort = 0
	app, err := Build(Config{Games: []GameConfig{{Game: d.Key, Bind: "127.0.0.1", PublicIP: "127.0.0.1"}}}, []game.Definition{d}, store.NewMemoryDB(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if len(app.TCP) != 1 || len(app.UDP) != 0 {
		t.Fatal("TCP-only capabilities ignored")
	}
}

func TestSharedDiscoveryUsesSameUDPListenersWithDifferentPolicies(t *testing.T) {
	tcpPort := availablePort(t)
	primary, alternate := availablePort(t), availablePort(t)
	for primary == alternate {
		alternate = availablePort(t)
	}
	app, err := buildForTest(Config{Games: []GameConfig{
		{Game: familyfeud.Key, Bind: "127.0.0.1", PublicIP: "127.0.0.1", TCPPort: tcpPort, STUN: Connectivity{ShareTransportWith: svr2007.Key}},
		{Game: outrun2006.Key, Bind: "127.0.0.1", PublicIP: "127.0.0.1", TCPPort: tcpPort, STUN: Connectivity{ShareWith: svr2007.Key}},
		{Game: svr2007.Key, Bind: "127.0.0.1", PublicIP: "127.0.0.1", TCPPort: tcpPort, STUN: Connectivity{Port: primary, AltPort: alternate, Mode: stun.Adaptive}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	target := net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: primary}
	for _, tt := range []struct {
		request      []byte
		size, source int
	}{
		{[]byte{0x1e, 0xa5, 1, 0}, 12, primary},
		{[]byte{0x14, 0, 1, 0, 2, 0, 0, 0}, 18, alternate},
		{[]byte{0x1e, 2, 0}, 9, primary},
		{[]byte{0x14, 2, 0, 2}, 15, primary},
	} {
		deadline := time.Now().Add(2 * time.Second)
		var n int
		var source *net.UDPAddr
		buf := make([]byte, 64)
		for time.Now().Before(deadline) {
			conn.SetDeadline(time.Now().Add(50 * time.Millisecond))
			_, err = conn.WriteToUDP(tt.request, &target)
			if err == nil {
				n, source, err = conn.ReadFromUDP(buf)
			}
			if err == nil {
				break
			}
		}
		if err != nil || n != tt.size || source.Port != tt.source {
			t.Fatalf("shared discovery request %x: size=%d source=%v err=%v", tt.request, n, source, err)
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shared listeners did not stop")
	}
}
