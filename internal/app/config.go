package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/princebyyourlogic/angelware/internal/connectivity/stun"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/auth"
)

type Config struct {
	DatabaseURL string       `json:"database_url"`
	AuthMode    auth.Mode    `json:"auth_mode"`
	LogLevel    string       `json:"log_level"`
	Games       []GameConfig `json:"games"`
}
type GameConfig struct {
	Game     string          `json:"game"`
	Bind     string          `json:"bind"`
	PublicIP string          `json:"public_ip"`
	TCPPort  int             `json:"tcp_port,omitempty"`
	STUN     Connectivity    `json:"stun"`
	Options  json.RawMessage `json:"options,omitempty"`
}
type Connectivity struct {
	ShareWith    string    `json:"share_with,omitempty"`
	Port         int       `json:"port,omitempty"`
	AltPort      int       `json:"alt_port,omitempty"`
	Mode         stun.Mode `json:"mode,omitempty"`
	RelayPorts   string    `json:"relay_ports,omitempty"`
	MaxClients   int       `json:"max_clients,omitempty"`
	DirectWindow float64   `json:"direct_window_seconds,omitempty"`
}

func LoadConfig() (Config, error) {
	path := os.Getenv("ANGELWARE_CONFIG")
	if path == "" {
		return legacy()
	}
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	var c Config
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, err
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return c, fmt.Errorf("config: expected one JSON document")
	}
	if c.DatabaseURL == "" {
		c.DatabaseURL = os.Getenv("ANGELWARE_DATABASE_URL")
	}
	return c, nil
}
