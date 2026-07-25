// Package config handles loading and validation of the agent configuration.
// Configuration is layered: defaults → config-drive "agent" metadata (see
// pkg/isoconfig) → environment variables. Environment variables use the
// prefix PLUSCLOUDS_AGENT_ and dot-separated keys map to underscores (e.g.
// nats.url → PLUSCLOUDS_AGENT_NATS_URL).
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// AgentVersion is set at build time via -ldflags.
const AgentVersion = "0.2.0"

// Config is the top-level configuration structure for the agent.
type Config struct {
	NATS     NATSConfig     `mapstructure:"nats"`
	Agent    AgentConfig    `mapstructure:"agent"`
	ISO      ISOConfig      `mapstructure:"iso"`
	Log      LogConfig      `mapstructure:"log"`
	Autoheal AutohealConfig `mapstructure:"autoheal"`
}

// Connection type constants for NATSConfig.ConnectionType.
const (
	ConnectionTypeNATS      = "nats"
	ConnectionTypeWebSocket = "websocket"
)

// NATSConfig holds NATS connection settings.
type NATSConfig struct {
	// ConnectionType selects the transport: "nats" (TCP, default) or "websocket".
	// Use "websocket" to connect through firewalls/proxies without TLS certificates.
	ConnectionType string `mapstructure:"connection_type"`
	// URL is the NATS TCP server address, used when connection_type is "nats".
	// Example: nats://nats.plusclouds.com:4222
	URL string `mapstructure:"url"`
	// WebSocketURL is the NATS WebSocket address, used when connection_type is "websocket".
	// Use ws:// for plain WebSocket (no TLS). Example: ws://nats.plusclouds.com:8080
	WebSocketURL string `mapstructure:"websocket_url"`
	// AgentUUID is the fallback VM UUID used as the NATS username when the ISO
	// config drive is not present. The ISO virtual_machine_id always takes precedence.
	// Can also be set via PLUSCLOUDS_AGENT_NATS_AGENT_UUID environment variable.
	AgentUUID string `mapstructure:"agent_uuid"`
	// APIKey is the fallback NATS authentication token when the ISO config drive
	// does not carry an agent_api_key. The ISO value always takes precedence.
	// Can also be set via PLUSCLOUDS_AGENT_NATS_API_KEY environment variable.
	APIKey string `mapstructure:"api_key"`
	// MaxReconnects controls how many times the client retries on disconnect.
	// -1 means unlimited.
	MaxReconnects int `mapstructure:"max_reconnects"`
	// ReconnectWait is the delay between reconnect attempts.
	ReconnectWait time.Duration `mapstructure:"reconnect_wait"`
}

// ActiveURL returns the server URL for the configured connection type.
func (c NATSConfig) ActiveURL() string {
	if c.ConnectionType == ConnectionTypeWebSocket {
		return c.WebSocketURL
	}
	return c.URL
}

// AgentConfig holds agent behaviour settings.
type AgentConfig struct {
	// HeartbeatInterval is how often the agent publishes a heartbeat event.
	HeartbeatInterval time.Duration `mapstructure:"heartbeat_interval"`
	// TelemetryInterval is how often the agent publishes a telemetry event.
	TelemetryInterval time.Duration `mapstructure:"telemetry_interval"`
	// AllowedOperations is the set of operation names the dispatcher will execute.
	// Any operation not listed is rejected without execution.
	AllowedOperations []string `mapstructure:"allowed_operations"`
	// AllowedCommands is the allowlist of binary paths the exec operation may run.
	// Only consulted when "exec" appears in AllowedOperations.
	AllowedCommands []string `mapstructure:"allowed_commands"`
}

// ISOConfig holds ISO/config-drive settings.
type ISOConfig struct {
	// MountPath is the filesystem path where the config drive ISO is mounted.
	MountPath string `mapstructure:"mount_path"`
	// FallbackEnv enables falling back to environment variables when ISO files
	// are absent. Useful for development and testing without a real ISO.
	FallbackEnv bool `mapstructure:"fallback_env"`
	// Label is the volume label the config-drive ISO is built with, used to
	// find it under /dev/disk/by-label (Linux) or by FileSystemLabel
	// (Windows) before mounting it.
	Label string `mapstructure:"label"`
	// CachePath is where the agent stores a local copy of pc-meta-data.json
	// after reading it from the config-drive, so identity/metadata survive
	// later boots where the config-drive isn't attached.
	CachePath string `mapstructure:"cache_path"`
}

// LogConfig holds logging settings.
type LogConfig struct {
	// Level is the minimum log level: debug, info, warn, error.
	Level string `mapstructure:"level"`
	// Format controls log output to stdout: json or console.
	Format string `mapstructure:"format"`
	// File is an optional path to write logs to in addition to stdout.
	// Always written in JSON format regardless of the Format setting.
	// Leave empty to disable file logging.
	File string `mapstructure:"file"`
}

// AutohealConfig holds automatic service recovery settings.
type AutohealConfig struct {
	// Enabled activates automatic restart of failed services.
	Enabled bool `mapstructure:"enabled"`
	// RestartDelay is how long to wait before restarting a failed service.
	RestartDelay time.Duration `mapstructure:"restart_delay"`
}

// Load builds the agent configuration from built-in defaults, overlaid with
// the "agent" JSON object from the config-drive metadata (pc-meta-data.json
// or its local cache — see pkg/isoconfig), overlaid with environment
// variables (prefix PLUSCLOUDS_AGENT_). agentSettings may be nil or empty,
// in which case the built-in defaults (and any env overrides) apply as-is —
// this is also how Load is called before the config-drive has been read, to
// bootstrap the iso.mount_path/label/cache_path needed to find it.
func Load(agentSettings json.RawMessage) (*Config, error) {
	v := viper.New()

	setDefaults(v)

	if len(bytes.TrimSpace(agentSettings)) > 0 && !bytes.Equal(bytes.TrimSpace(agentSettings), []byte("null")) {
		var m map[string]any
		if err := json.Unmarshal(agentSettings, &m); err != nil {
			return nil, fmt.Errorf("parsing agent settings from config-drive metadata: %w", err)
		}
		if err := v.MergeConfigMap(m); err != nil {
			return nil, fmt.Errorf("merging agent settings from config-drive metadata: %w", err)
		}
	}

	v.SetEnvPrefix("PLUSCLOUDS_AGENT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("nats.connection_type", ConnectionTypeNATS)
	v.SetDefault("nats.url", "nats://localhost:4222")
	v.SetDefault("nats.websocket_url", "ws://localhost:8080")
	v.SetDefault("nats.agent_uuid", "")
	v.SetDefault("nats.api_key", "")
	v.SetDefault("nats.max_reconnects", -1)
	v.SetDefault("nats.reconnect_wait", 5*time.Second)

	v.SetDefault("agent.heartbeat_interval", 30*time.Second)
	v.SetDefault("agent.telemetry_interval", 30*time.Second)
	v.SetDefault("agent.allowed_operations", []string{
		"agent.allowed_operations",
		"agent.version",
		"services.list",
		"services.get",
		"services.start",
		"services.stop",
		"services.restart",
		"services.reload",
		"services.enable",
		"services.disable",
		"system.info",
		"system.metrics",
		"system.cpu",
		"system.memory",
		"system.disk",
		"system.network",
		"system.update",
		"telemetry.set_interval",
		"vm.reboot",
		"vm.shutdown",
	})
	v.SetDefault("agent.allowed_commands", []string{})

	v.SetDefault("iso.mount_path", "/media/plusclouds-config")
	v.SetDefault("iso.fallback_env", true)
	// "cidata" is the volume label the platform actually builds the
	// config-drive with (a standard cloud-init NoCloud ISO, carrying
	// pc-meta-data.json alongside the usual meta-data/user-data) — not
	// "plusclouds-config", which is only the local mount directory name.
	v.SetDefault("iso.label", "cidata")
	v.SetDefault("iso.cache_path", "/var/lib/plusclouds/cache/pc-meta-data.json")

	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
	v.SetDefault("log.file", "")

	v.SetDefault("autoheal.enabled", true)
	v.SetDefault("autoheal.restart_delay", 10*time.Second)
}
