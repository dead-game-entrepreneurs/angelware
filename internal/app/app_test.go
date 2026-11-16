package app

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/princebyyourlogic/angelware/internal/connectivity/stun"
	"github.com/princebyyourlogic/angelware/internal/game"
	"github.com/princebyyourlogic/angelware/internal/games/codbro"
	"github.com/princebyyourlogic/angelware/internal/games/familyfeud"
	"github.com/princebyyourlogic/angelware/internal/games/outrun2006"
	"github.com/princebyyourlogic/angelware/internal/games/svr2007"
	"github.com/princebyyourlogic/angelware/internal/games/wsop2008"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/frame"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

func definitions() []game.Definition {
	return []game.Definition{svr2007.Definition(), outrun2006.Definition(), codbro.Definition(), familyfeud.Definition(), wsop2008.Definition()}
}

func TestCommonSDKTransportAndDiscoveryOnlyChild(t *testing.T) {
	config := Config{Games: []GameConfig{
		{Game: familyfeud.Key, Bind: "127.0.0.1", PublicIP: "127.0.0.1", STUN: Connectivity{ShareTransportWith: svr2007.Key}},
		{Game: outrun2006.Key, Bind: "127.0.0.1", PublicIP: "127.0.0.1", STUN: Connectivity{ShareWith: svr2007.Key}},
		{Game: svr2007.Key, Bind: "127.0.0.1", PublicIP: "127.0.0.1", STUN: Connectivity{Mode: stun.Adaptive}, Options: []byte(`{"relay_rewrite":true,"observer":true}`)},
	}}
	app, err := buildForTest(config)
	if err != nil {
		t.Fatal(err)
	}
	if len(app.UDP) != 1 || len(app.TCP) != 1 || len(app.TCP["127.0.0.1:3074"].Auth) != 3 {
		t.Fatal("same-version SDK sharing created duplicate listeners or lost title routing")
	}
	if app.UDP[0].Mode != stun.Adaptive || len(app.UDP[0].Games) != 2 {
		t.Fatal("SDK transport did not retain its policy/members")
	}
	// The socket owner need not be the title that installs a peer hook. Resolve
	// aliases before factories run, including when the hook belongs to a child.
	reversed := config
	reversed.Games = append([]GameConfig(nil), config.Games...)
	reversed.Games[0].STUN = Connectivity{Mode: stun.Adaptive}
	reversed.Games[1].STUN = Connectivity{ShareWith: familyfeud.Key}
	reversed.Games[2].STUN = Connectivity{ShareTransportWith: familyfeud.Key}
	if app, err := buildForTest(reversed); err != nil || len(app.UDP) != 1 || app.UDP[0].Games[0] != familyfeud.Key {
		t.Fatalf("SDK owner reversal failed: %v", err)
	}
	for name, edit := range map[string]func(*Config){
		"missing owner": func(c *Config) { c.Games = c.Games[:1] },
		"self":          func(c *Config) { c.Games[0].STUN.ShareTransportWith = familyfeud.Key },
		"cycle":         func(c *Config) { c.Games[2].STUN = Connectivity{ShareTransportWith: familyfeud.Key} },
		"version mismatch": func(c *Config) {
			c.Games = append(c.Games, GameConfig{Game: "codbro", Bind: "127.0.0.1", PublicIP: "127.0.0.1"})
			c.Games[0].STUN.ShareTransportWith = "codbro"
		},
		"native owner":      func(c *Config) { c.Games[0].STUN.ShareTransportWith = outrun2006.Key },
		"native child":      func(c *Config) { c.Games[1].STUN = Connectivity{ShareTransportWith: svr2007.Key} },
		"override":          func(c *Config) { c.Games[0].STUN.Mode = stun.Relay },
		"different bind":    func(c *Config) { c.Games[0].Bind = "127.0.0.2" },
		"different public":  func(c *Config) { c.Games[0].PublicIP = "127.0.0.2" },
		"two sharing modes": func(c *Config) { c.Games[0].STUN.ShareWith = svr2007.Key },
	} {
		t.Run(name, func(t *testing.T) {
			c := config
			c.Games = append([]GameConfig(nil), config.Games...)
			edit(&c)
			if _, err := buildForTest(c); err == nil {
				t.Fatal("ambiguous SDK sharing accepted")
			}
		})
	}
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

func TestMMP3SharesExistingListeners(t *testing.T) {
	c := Config{Games: []GameConfig{
		{Game: svr2007.Key, Bind: "127.0.0.1", PublicIP: "127.0.0.1", STUN: Connectivity{Mode: stun.Adaptive}},
		{Game: familyfeud.Key, Bind: "127.0.0.1", PublicIP: "127.0.0.1", STUN: Connectivity{ShareTransportWith: svr2007.Key}},
		{Game: wsop2008.Key, Bind: "127.0.0.1", PublicIP: "127.0.0.1", STUN: Connectivity{ShareTransportWith: svr2007.Key}},
	}}
	a, err := buildForTest(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.TCP) != 1 || len(a.UDP) != 1 || len(a.UDP[0].Games) != 3 {
		t.Fatal("MMP3 created duplicate listeners")
	}
	auth := a.TCP["127.0.0.1:3074"].Auth
	if auth[wsop2008.TitleID].Protocol != frame.MMP3 || auth[svr2007.TitleID].Protocol != frame.Legacy || auth[familyfeud.TitleID].Protocol != frame.Legacy {
		t.Fatal("incorrect protocol binding")
	}
}
