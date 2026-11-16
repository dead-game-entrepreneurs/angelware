package stun

import (
	"fmt"
	"log/slog"
	"net/netip"
	"time"
)

// AddDiscovery attaches a discovery-only game to these sockets before Run.
// Its direct router has its own admission/peer state and no SDK relay allocator.
// Wire versions must be distinct; DNS names are not present in UDP datagrams.
func (s *Server) AddDiscovery(version uint16, log *slog.Logger) error {
	if s.DiscoveryVersion < 1 || s.DiscoveryVersion > 2 || version < 1 || version > 2 || version == s.DiscoveryVersion {
		return fmt.Errorf("shared discovery requires distinct supported SDK wire versions")
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
