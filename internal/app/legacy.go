// Legacy environment compatibility is intentionally kept at the configuration edge.
package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/princebyyourlogic/angelware/internal/connectivity/stun"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/auth"
)

type legacyConfig struct {
	PublicIP        netip.Addr
	Bind            string
	Port            int
	STUNAltPort     int
	DatabaseURL     string
	AuthMode        auth.Mode
	LogLevel        slog.Level
	STUN            []stunTitle
	SVRRelayRewrite bool
	SVRObserver     bool
}

type stunTitle struct {
	name string
	stun.Config
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func loadLegacyConfig() (legacyConfig, error) {
	c := legacyConfig{Bind: env("ANGELWARE_BIND", "0.0.0.0"), DatabaseURL: os.Getenv("ANGELWARE_DATABASE_URL"),
		AuthMode: auth.Mode(env("ANGELWARE_AUTH_MODE", string(auth.Verify)))}
	var err error
	if c.PublicIP, err = netip.ParseAddr(os.Getenv("ANGELWARE_PUBLIC_IP")); err != nil || !c.PublicIP.Is4() {
		return c, errors.New("ANGELWARE_PUBLIC_IP must be the server's reachable IPv4 address")
	}
	if c.Port, err = strconv.Atoi(env("ANGELWARE_PORT", "3074")); err != nil {
		return c, fmt.Errorf("ANGELWARE_PORT: %w", err)
	}
	if c.STUNAltPort, err = strconv.Atoi(env("ANGELWARE_STUN_ALT_PORT", "3075")); err != nil {
		return c, fmt.Errorf("ANGELWARE_STUN_ALT_PORT: %w", err)
	}
	if c.AuthMode != auth.Verify && c.AuthMode != auth.Open {
		return c, fmt.Errorf("ANGELWARE_AUTH_MODE must be %q or %q", auth.Verify, auth.Open)
	}
	if err := c.LogLevel.UnmarshalText([]byte(env("ANGELWARE_LOG_LEVEL", "info"))); err != nil {
		return c, fmt.Errorf("ANGELWARE_LOG_LEVEL: %w", err)
	}
	switch env("ANGELWARE_SVR_RELAY_REWRITE", "off") {
	case "on":
		c.SVRRelayRewrite = true
	case "off":
	default:
		return c, errors.New("ANGELWARE_SVR_RELAY_REWRITE must be on or off")
	}
	switch env("ANGELWARE_SVR_OBSERVER", "off") {
	case "on":
		c.SVRObserver = true
	case "off":
	default:
		return c, errors.New("ANGELWARE_SVR_OBSERVER must be on or off")
	}
	if err := c.loadSTUN(); err != nil {
		return c, err
	}
	if c.SVRObserver {
		if !c.SVRRelayRewrite || c.STUN[0].Mode != stun.Adaptive {
			return c, errors.New("SvR observer requires adaptive mode and ANGELWARE_SVR_RELAY_REWRITE=on")
		}
	}
	return c, nil
}

// loadSTUN configures one STUN service per title; they share the public IP and
// the relay port range.
func (c *legacyConfig) loadSTUN() error {
	shared := stun.Config{PublicIP: c.PublicIP}
	var err error
	if shared.RelayLo, shared.RelayHi, err = portRange(env("ANGELWARE_RELAY_PORTS", "20000-20199")); err != nil {
		return fmt.Errorf("ANGELWARE_RELAY_PORTS: %w", err)
	}
	if shared.RelayMaxClients, err = strconv.Atoi(env("ANGELWARE_RELAY_MAX_CLIENTS", "64")); err != nil {
		return fmt.Errorf("ANGELWARE_RELAY_MAX_CLIENTS: %w", err)
	}
	window, err := strconv.ParseFloat(env("ANGELWARE_DIRECT_WINDOW", "1.0"), 64)
	if err != nil {
		return fmt.Errorf("ANGELWARE_DIRECT_WINDOW: %w", err)
	}
	shared.DirectWindow = time.Duration(window * float64(time.Second))

	svr := shared
	svr.Port, svr.AltPort = c.Port, c.STUNAltPort
	svr.Mode = stun.Mode(env("ANGELWARE_STUN_MODE", string(stun.Direct)))
	c.STUN = append(c.STUN, stunTitle{"svr2007", svr})

	if env("ANGELWARE_BRO", "on") != "off" {
		bro := shared
		bro.Mode = stun.Mode(env("ANGELWARE_BRO_MODE", string(stun.Relay)))
		if bro.Port, err = strconv.Atoi(env("ANGELWARE_BRO_PORT", "2273")); err != nil {
			return fmt.Errorf("ANGELWARE_BRO_PORT: %w", err)
		}
		if bro.AltPort, err = strconv.Atoi(env("ANGELWARE_BRO_ALT_PORT", "2274")); err != nil {
			return fmt.Errorf("ANGELWARE_BRO_ALT_PORT: %w", err)
		}
		c.STUN = append(c.STUN, stunTitle{"cod-bro", bro})
	}
	for _, t := range c.STUN {
		if err := t.Validate(); err != nil {
			return fmt.Errorf("%s stun: %w", t.name, err)
		}
	}
	return nil
}

func portRange(s string) (lo, hi int, err error) {
	a, b, ok := strings.Cut(s, "-")
	if !ok {
		return 0, 0, errors.New("want LOW-HIGH")
	}
	if lo, err = strconv.Atoi(a); err == nil {
		hi, err = strconv.Atoi(b)
	}
	return lo, hi, err
}

func legacy() (Config, error) {
	c, err := loadLegacyConfig()
	if err != nil {
		return Config{}, err
	}
	out := Config{DatabaseURL: c.DatabaseURL, AuthMode: c.AuthMode, LogLevel: c.LogLevel.String()}
	for _, t := range c.STUN {
		key := t.name
		if key == "cod-bro" {
			key = "codbro"
		}
		settings := json.RawMessage(nil)
		port := 0
		if key == "svr2007" {
			port = c.Port
			settings, _ = json.Marshal(map[string]bool{"relay_rewrite": c.SVRRelayRewrite, "observer": c.SVRObserver})
		}
		out.Games = append(out.Games, GameConfig{Game: key, Bind: c.Bind, PublicIP: c.PublicIP.String(), TCPPort: port,
			STUN: Connectivity{Port: t.Port, AltPort: t.AltPort, Mode: t.Mode, RelayPorts: fmt.Sprintf("%d-%d", t.RelayLo, t.RelayHi), MaxClients: t.RelayMaxClients, DirectWindow: t.DirectWindow.Seconds()}, Options: settings})
	}
	return out, nil
}
