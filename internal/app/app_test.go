package app

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/princebyyourlogic/angelware/internal/connectivity/stun"
	"github.com/princebyyourlogic/angelware/internal/game"
	"github.com/princebyyourlogic/angelware/internal/games/codbro"
	"github.com/princebyyourlogic/angelware/internal/games/outrun2006"
	"github.com/princebyyourlogic/angelware/internal/games/svr2007"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

func definitions() []game.Definition {
	return []game.Definition{svr2007.Definition(), outrun2006.Definition(), codbro.Definition()}
}
func buildForTest(c Config) (*App, error) {
	return Build(c, definitions(), store.NewMemoryDB(), slog.New(slog.NewTextHandler(io.Discard, nil)))
}
func TestBuildComposesGamesAndSharedTCP(t *testing.T) {
	c := Config{Games: []GameConfig{
		{Game: svr2007.Key, Bind: "127.0.0.1", PublicIP: "127.0.0.1"},
		{Game: outrun2006.Key, Bind: "127.0.0.1", PublicIP: "127.0.0.1", STUN: Connectivity{Port: 13074, AltPort: 13075}},
		{Game: "codbro", Bind: "127.0.0.1", PublicIP: "127.0.0.1"},
	}}
	app, err := buildForTest(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(app.TCP) != 1 || len(app.UDP) != 3 {
		t.Fatal("capabilities did not determine listeners")
	}
	tcp := app.TCP["127.0.0.1:3074"]
	if len(tcp.Auth) != 2 || len(tcp.Lobby.Applications) != 2 {
		t.Fatal("TCP title routes missing")
	}
	if tcp.Auth[svr2007.TitleID].Store == tcp.Auth[outrun2006.TitleID].Store {
		t.Fatal("games share store handle")
	}
	if tcp.Lobby.Applications[svr2007.Key] == tcp.Lobby.Applications[outrun2006.Key] {
		t.Fatal("games share transient state")
	}
	if app.UDP[2].Mode != stun.Relay || app.UDP[1].Mode != stun.Direct {
		t.Fatal("game defaults lost")
	}
}
func TestBuildRejectsAmbiguousOrUnsupportedConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name  string
		games []GameConfig
		want  string
	}{
		{"unknown", []GameConfig{{Game: "unknown"}}, "unknown game"},
		{"duplicate", []GameConfig{{Game: svr2007.Key, PublicIP: "127.0.0.1"}, {Game: svr2007.Key, PublicIP: "127.0.0.1"}}, "duplicate enabled"},
		{"overlap", []GameConfig{{Game: svr2007.Key, PublicIP: "127.0.0.1"}, {Game: outrun2006.Key, PublicIP: "127.0.0.2"}}, "UDP"},
		{"wildcard", []GameConfig{{Game: svr2007.Key, Bind: "0.0.0.0", PublicIP: "127.0.0.1"}, {Game: outrun2006.Key, Bind: "127.0.0.2", PublicIP: "127.0.0.2"}}, "UDP"},
		{"outrun relay", []GameConfig{{Game: outrun2006.Key, PublicIP: "127.0.0.1", STUN: Connectivity{Mode: stun.Relay}}}, "direct"},
		{"invalid options", []GameConfig{{Game: svr2007.Key, PublicIP: "127.0.0.1", Options: []byte(`{"typo":true}`)}}, "unknown field"},
		{"stun only", []GameConfig{{Game: "codbro", PublicIP: "127.0.0.1", TCPPort: 3074}}, "no TCP"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildForTest(Config{Games: tt.games})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want %q, got %v", tt.want, err)
			}
		})
	}
}
func TestNativePortsOnSeparateAddresses(t *testing.T) {
	app, err := buildForTest(Config{Games: []GameConfig{{Game: svr2007.Key, Bind: "127.0.0.1", PublicIP: "127.0.0.1"}, {Game: outrun2006.Key, Bind: "127.0.0.2", PublicIP: "127.0.0.2"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(app.TCP) != 2 || len(app.UDP) != 2 {
		t.Fatal("separate native endpoints not composed")
	}
}

func TestOptionalNativeTransportAndPortConflicts(t *testing.T) {
	config := Config{Games: []GameConfig{{Game: outrun2006.Key, Bind: "127.0.0.1", PublicIP: "127.0.0.1", Options: []byte(`{"native_relay":true}`)}}}
	app, err := buildForTest(config)
	if err != nil {
		t.Fatal(err)
	}
	if len(app.Services) != 1 || app.Services[0].UDPAddress().String() != "127.0.0.1:3658" {
		t.Fatal("native relay not composed")
	}
	config.Games[0].STUN = Connectivity{Port: 3658, AltPort: 3659}
	if _, err = buildForTest(config); err == nil || !strings.Contains(err.Error(), "UDP") {
		t.Fatalf("missing native UDP conflict: %v", err)
	}
}

func TestSharedDiscoveryResolvesAfterOwnerAndPreservesGameIsolation(t *testing.T) {
	config := Config{Games: []GameConfig{
		{Game: outrun2006.Key, Bind: "127.0.0.1", PublicIP: "127.0.0.1", STUN: Connectivity{ShareWith: svr2007.Key}, Options: []byte(`{"native_relay":true}`)},
		{Game: svr2007.Key, Bind: "127.0.0.1", PublicIP: "127.0.0.1", STUN: Connectivity{Mode: stun.Adaptive}},
	}}
	app, err := buildForTest(config)
	if err != nil {
		t.Fatal(err)
	}
	if len(app.TCP) != 1 || len(app.UDP) != 1 || len(app.Services) != 1 {
		t.Fatal("shared discovery bound duplicate sockets or lost native relay")
	}
	if len(app.TCP["127.0.0.1:3074"].Auth) != 2 {
		t.Fatal("shared TCP title routing lost")
	}
	for name, edit := range map[string]func(*Config){
		"missing owner":     func(c *Config) { c.Games = c.Games[:1] },
		"override":          func(c *Config) { c.Games[0].STUN.Port = 13074 },
		"different address": func(c *Config) { c.Games[0].Bind = "127.0.0.2" },
		"traversal owner":   func(c *Config) { c.Games[1].STUN = Connectivity{ShareWith: outrun2006.Key} },
	} {
		t.Run(name, func(t *testing.T) {
			c := config
			c.Games = append([]GameConfig(nil), config.Games...)
			edit(&c)
			if _, err := buildForTest(c); err == nil {
				t.Fatal("ambiguous discovery sharing accepted")
			}
		})
	}
}
