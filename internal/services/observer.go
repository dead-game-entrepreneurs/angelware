package services

import (
	"net"
	"net/netip"
	"time"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
)

// ConnectivityEvent is a stock host's authenticated native-count transition,
// scoped to one room. It is not a GSS game-address test. UniqueGuest is false
// when room-key disclosure cannot identify exactly one other gateway client.
type ConnectivityEvent struct {
	Room                 [8]byte
	Host                 netip.AddrPort
	GuestIP              netip.Addr
	Started, At          time.Time
	Created, UniqueGuest bool
}

func gatewayIP(owner any) (netip.Addr, bool) {
	var addr net.Addr
	if c, ok := owner.(interface{ RemoteAddr() net.Addr }); ok {
		addr = c.RemoteAddr()
	}
	if addr == nil {
		return netip.Addr{}, false
	}
	ip, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return netip.Addr{}, false
	}
	a, err := netip.ParseAddr(ip)
	return a.Unmap(), err == nil
}

func (s *mmSession) noteDisclosure(owner any) {
	if owner == s.owner {
		return
	}
	if s.disclosed == nil {
		s.disclosed = owner
		s.disclosedIP, _ = gatewayIP(owner)
	} else if s.disclosed != owner {
		s.ambiguous = true
	}
}

func (s *mmSession) connectivityEvent(id [8]byte, info []bitbuf.Field, at time.Time, inspect func([]bitbuf.Field) (netip.AddrPort, int, bool)) (ConnectivityEvent, bool) {
	e := ConnectivityEvent{Room: id, Started: s.createdAt, At: at, GuestIP: s.disclosedIP, UniqueGuest: s.disclosed != nil && !s.ambiguous && s.disclosedIP.IsValid()}
	if inspect == nil || s.createdAt.IsZero() {
		return e, false
	}
	host, _, valid := inspect(info)
	original, _, originalValid := inspect(s.info)
	if !valid || !originalValid || host != original {
		return e, false
	}
	e.Host = host
	ownerIP, ok := gatewayIP(s.owner)
	if !ok || e.Host.Port() == 0 || e.Host.Addr() != ownerIP {
		return e, false
	}
	return e, true
}
