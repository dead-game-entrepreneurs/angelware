package stun

import (
	"fmt"
	"log/slog"
	"net/netip"
	"sort"
	"time"
)

// RegisterPeerRewriter installs a game-owned hook before Run. Each hook must
// verify its own room/key binding; sharing a socket never shares room lookup.
func (s *Server) RegisterPeerRewriter(game string, rewrite PeerRewriter) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.router != nil || game == "" || rewrite == nil {
		return fmt.Errorf("peer rewriter requires a game and must be registered before Run")
	}
	if s.rewriters == nil {
		s.rewriters = map[string]PeerRewriter{}
	}
	if s.rewriters[game] != nil {
		return fmt.Errorf("duplicate peer rewriter for %s", game)
	}
	s.rewriters[game] = rewrite
	return nil
}

// Called with s.mu held. Snapshot the hooks so packet forwarding never consults
// mutable startup configuration, and config ordering cannot change hook order.
func (s *Server) peerRewriter() PeerRewriter {
	var hooks []PeerRewriter
	if s.Config.RewritePeer != nil {
		hooks = append(hooks, s.Config.RewritePeer)
	}
	var games []string
	for game := range s.rewriters {
		games = append(games, game)
	}
	sort.Strings(games)
	for _, game := range games {
		hooks = append(hooks, s.rewriters[game])
	}
	if len(hooks) == 0 {
		return nil
	}
	return func(sender, destination int, senderControl, senderVirtual, destinationControl, destinationVirtual netip.AddrPort, packet []byte, now time.Time) []byte {
		for _, hook := range hooks {
			packet = hook(sender, destination, senderControl, senderVirtual, destinationControl, destinationVirtual, packet, now)
			if packet == nil {
				return nil
			}
		}
		return packet
	}
}

// AddTransport attaches another SDK discovery version to the common traversal
// and relay state. Both supported discovery versions use packed-v2 traversal.
func (s *Server) AddTransport(version uint16) error {
	if s.DiscoveryVersion < 1 || s.DiscoveryVersion > 2 || version < 1 || version > 2 {
		return fmt.Errorf("shared transport requires supported SDK discovery versions")
	}
	if s.profiles[version] != nil && s.Mode == Relay {
		return fmt.Errorf("discovery-only and SDK clients with version %d require direct or adaptive mode; relay mode advertises virtual addresses", version)
	}
	if s.transports == nil {
		s.transports = map[uint16]bool{}
	}
	s.transports[version] = true
	return nil
}

func (s *Server) sdkVersion(version uint16) bool {
	return version == s.DiscoveryVersion || s.transports[version]
}

// AddDiscovery attaches a discovery-only game to these sockets before Run.
// A distinct version keeps an independent direct router. If an SDK member uses
// the same version, discovery must use their common real-address policy: there
// is no title/domain identifier in these packets. Native gameplay stays separate.
func (s *Server) AddDiscovery(version uint16, log *slog.Logger) error {
	if s.DiscoveryVersion < 1 || s.DiscoveryVersion > 2 || version < 1 || version > 2 {
		return fmt.Errorf("shared discovery requires supported SDK wire versions")
	}
	if s.sdkVersion(version) && s.Mode == Relay {
		return fmt.Errorf("discovery-only and SDK clients with version %d require direct or adaptive mode; relay mode advertises virtual addresses", version)
	}
	if s.profiles == nil {
		s.profiles = map[uint16]*slog.Logger{}
	}
	if _, exists := s.profiles[version]; exists {
		return fmt.Errorf("discovery wire version %d is already assigned", version)
	}
	s.profiles[version] = log
	return nil
}
func (s *Server) initDiscovery() {
	s.discovery = map[uint16]*Router{}
	for version, log := range s.profiles {
		if s.sdkVersion(version) {
			log.Info("discovery uses shared SDK policy", "udp", []int{s.Port, s.AltPort}, "version", version, "mode", s.Mode)
			continue
		}
		cfg := s.Config
		cfg.Mode = Direct
		cfg.RewritePeer = nil
		s.discovery[version] = NewRouter(cfg, log, nil, nil)
		log.Info("discovery listener shared", "udp", []int{s.Port, s.AltPort}, "version", version)
	}
}

// Called under s.mu. IP/NAT packets identify the SDK version. Traversal always
// uses packed v2, so subsequent packets follow the endpoint learned by discovery.
// An explicit discovery in a different version updates that endpoint's owner.
func (s *Server) routeDiscovery(b []byte, from netip.AddrPort, on Listener, now time.Time) []Outbound {
	if len(s.discovery) == 0 {
		return s.router.Route(b, from, on, now)
	}
	req, err := parse(b)
	if err != nil {
		return nil
	}
	if req.kind == ipRequest || req.kind == natRequest {
		target := s.router
		if shared := s.discovery[req.version]; shared != nil {
			target = shared
		}
		out := target.Route(b, from, on, now)
		if target.peers[from] == now {
			if target != s.router {
				delete(s.router.peers, from)
			}
			for _, other := range s.discovery {
				if other != target {
					delete(other.peers, from)
				}
			}
		}
		return out
	}
	for _, shared := range s.discovery {
		if seen, ok := shared.peers[from]; ok && now.Sub(seen) <= peerTTL {
			return shared.Route(b, from, on, now)
		}
	}
	return s.router.Route(b, from, on, now)
}
