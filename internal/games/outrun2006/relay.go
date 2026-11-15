package outrun2006

import (
	"context"
	"net/netip"

	"github.com/princebyyourlogic/angelware/internal/games/outrun2006/peer"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/gateway"
)

type relayLobby struct {
	gateway.Application
	relay *peer.Relay
}
type relayHandler struct {
	gateway.Handler
	client     gateway.Client
	relay      *peer.Relay
	authorized bool
}

func (l relayLobby) Open(ctx context.Context, c gateway.Client) gateway.Handler {
	return &relayHandler{Handler: l.Application.Open(ctx, c), client: c, relay: l.relay}
}
func (h *relayHandler) Handle(ctx context.Context, t gateway.Task) []byte {
	if !h.authorized {
		if address, err := netip.ParseAddrPort(h.client.RemoteAddr().String()); err == nil {
			h.relay.Authorize(peer.Token(h.client.Session().Token), address.Addr().Unmap(), h)
			h.authorized = true
		}
	}
	return h.Handler.Handle(ctx, t)
}
func (h *relayHandler) Close(ctx context.Context) {
	h.Handler.Close(ctx)
	if h.authorized {
		h.relay.Revoke(peer.Token(h.client.Session().Token), h)
	}
}
