package outrun2006

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/princebyyourlogic/angelware/internal/game"
	"github.com/princebyyourlogic/angelware/internal/games/outrun2006/peer"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/gateway"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/session"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

type relayTestClient struct{ value session.Session }

func (c relayTestClient) Session() session.Session { return c.value }

func (relayTestClient) RemoteAddr() net.Addr {
	return net.UDPAddrFromAddrPort(netip.MustParseAddrPort("192.0.2.1:1234"))
}
func (relayTestClient) Log() *slog.Logger          { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
func (relayTestClient) Send(uint8, []byte) error   { return nil }
func (relayTestClient) TrySend(uint8, []byte) bool { return true }

type noopLobby struct{}

func (noopLobby) Open(context.Context, gateway.Client) gateway.Handler { return noopHandler{} }

type noopHandler struct{}

func (noopHandler) Handle(context.Context, gateway.Task) []byte { return nil }
func (noopHandler) Close(context.Context)                       {}
func TestNativeTokenRequiresEncryptedTaskAndRevokesOnClose(t *testing.T) {
	relay := peer.New(netip.MustParseAddrPort("127.0.0.1:3658"), nil)
	client := relayTestClient{value: session.Session{Token: [16]byte{1}}}
	h := (relayLobby{Application: noopLobby{}, relay: relay}).Open(context.Background(), client)
	ep := netip.MustParseAddrPort("192.0.2.1:3658")
	register := peer.Frame(peer.Register, peer.Token(client.value.Token), netip.Addr{}, nil)
	if _, out := relay.Handle(ep, register, time.Now()); out != nil {
		t.Fatal("plain hello authorized native transport")
	}
	h.Handle(context.Background(), gateway.Task{})
	if _, out := relay.Handle(ep, register, time.Now()); out == nil {
		t.Fatal("encrypted task did not authorize native transport")
	}
	h.Close(context.Background())
	if _, out := relay.Handle(ep, register, time.Now()); out != nil {
		t.Fatal("closed gateway left native transport authorized")
	}
}

// Exercise the composed game, rather than granting peer admission directly.
func TestMatchmakingDisclosesNativePeersAndHostCloseRevokes(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	runtime, err := New(game.Options{Bind: netip.MustParseAddr("127.0.0.1"), Log: log, Store: store.NewMemoryDB().ForGame(Key), Sessions: session.NewRegistry(), Settings: []byte(`{"native_relay":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	relay := runtime.Services[0].(*peer.Relay)
	host := runtime.Lobby.Open(context.Background(), relayTestClient{value: session.Session{Token: [16]byte{1}, UserID: 1}})
	guest := runtime.Lobby.Open(context.Background(), relayTestClient{value: session.Session{Token: [16]byte{2}, UserID: 2}})
	defer guest.Close(context.Background())
	task := func(id uint8, fields []bitbuf.Field) gateway.Task {
		t.Helper()
		args, err := bitbuf.NewReader(bitbuf.NewWriter(true).U8(0).Fields(fields).Bytes())
		if err != nil {
			t.Fatal(err)
		}
		return gateway.Task{Service: 5, ID: id, Args: args}
	}
	// Initial authenticated tasks authorize transport but reveal no rooms.
	host.Handle(context.Background(), task(5, nil))
	guest.Handle(context.Background(), task(5, nil))
	he := netip.MustParseAddrPort("192.0.2.1:40001")
	ge := netip.MustParseAddrPort("192.0.2.1:40002")
	now := time.Now()
	_, ha := relay.Handle(he, peer.Frame(peer.Register, peer.Token{1}, netip.Addr{}, nil), now)
	_, ga := relay.Handle(ge, peer.Frame(peer.Register, peer.Token{2}, netip.Addr{}, nil), now)
	if ha == nil || ga == nil {
		t.Fatal("composed lobby did not authorize registration")
	}
	hostIP := netip.AddrFrom4([4]byte(ha[24:28]))
	packet := peer.Frame(peer.Data, peer.Token{2}, hostIP, []byte{0, 0, 0x12})
	host.Handle(context.Background(), task(1, room()))
	if _, out := relay.Handle(ge, packet, now); out != nil {
		t.Fatal("room creation alone admitted guest")
	}
	query := []bitbuf.Field{bitbuf.Int(bitbuf.I32, 1), bitbuf.Int(bitbuf.I32, 50)}
	for range 13 {
		query = append(query, bitbuf.Int(bitbuf.I32, 0x7fffffff))
	}
	guest.Handle(context.Background(), task(5, query))
	if dst, out := relay.Handle(ge, packet, now); dst != he || out == nil {
		t.Fatal("search disclosure did not admit native probe")
	}
	host.Close(context.Background())
	if _, out := relay.Handle(ge, packet, now); out != nil {
		t.Fatal("host disconnect did not revoke room")
	}
}
