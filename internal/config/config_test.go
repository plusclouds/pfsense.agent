package config_test

import (
	"testing"
	"time"

	"github.com/plusclouds/ubuntu-agent/internal/config"
	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

// --- Defaults ---

func TestLoad_Defaults(t *testing.T) {
	cfg, err := config.Load(nil)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.NATS.URL != "nats://localhost:4222" {
		t.Errorf("NATS URL default: got %q, want nats://localhost:4222", cfg.NATS.URL)
	}
	if cfg.NATS.MaxReconnects != -1 {
		t.Errorf("NATS max_reconnects default: got %d, want -1", cfg.NATS.MaxReconnects)
	}
	if cfg.NATS.ReconnectWait != 5*time.Second {
		t.Errorf("NATS reconnect_wait default: got %v, want 5s", cfg.NATS.ReconnectWait)
	}
	if cfg.Agent.HeartbeatInterval != 30*time.Second {
		t.Errorf("heartbeat interval default: got %v, want 30s", cfg.Agent.HeartbeatInterval)
	}
	if cfg.Agent.TelemetryInterval != 30*time.Second {
		t.Errorf("telemetry interval default: got %v, want 30s", cfg.Agent.TelemetryInterval)
	}
	if len(cfg.Agent.AllowedOperations) == 0 {
		t.Error("allowed_operations should have defaults")
	}
	if cfg.ISO.MountPath != "/media/plusclouds-config" {
		t.Errorf("ISO mount path default: got %q", cfg.ISO.MountPath)
	}
	if !cfg.ISO.FallbackEnv {
		t.Error("ISO fallback_env should default to true")
	}
	if cfg.ISO.Label != "plusclouds-config" {
		t.Errorf("ISO label default: got %q, want plusclouds-config", cfg.ISO.Label)
	}
	if cfg.ISO.CachePath != "/var/lib/plusclouds/cache/pc-meta-data.json" {
		t.Errorf("ISO cache_path default: got %q", cfg.ISO.CachePath)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("log level default: got %q, want info", cfg.Log.Level)
	}
	if cfg.Log.Format != "json" {
		t.Errorf("log format default: got %q, want json", cfg.Log.Format)
	}
	if !cfg.Autoheal.Enabled {
		t.Error("autoheal.enabled should default to true")
	}
	if cfg.Autoheal.RestartDelay != 10*time.Second {
		t.Errorf("autoheal restart_delay default: got %v, want 10s", cfg.Autoheal.RestartDelay)
	}
}

// --- Config-drive "agent" settings ---

func TestLoad_FromISOAgentSettings(t *testing.T) {
	agentJSON := `{
		"nats": {
			"url": "nats://nats.example.com:4222",
			"reconnect_wait": "10s"
		},
		"log": {
			"level": "debug",
			"format": "console"
		},
		"agent": {
			"heartbeat_interval": "60s",
			"telemetry_interval": "120s"
		}
	}`

	cfg, err := config.Load([]byte(agentJSON))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.NATS.URL != "nats://nats.example.com:4222" {
		t.Errorf("NATS URL: got %q", cfg.NATS.URL)
	}
	if cfg.NATS.ReconnectWait != 10*time.Second {
		t.Errorf("NATS reconnect_wait: got %v, want 10s", cfg.NATS.ReconnectWait)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("log level: got %q, want debug", cfg.Log.Level)
	}
	if cfg.Log.Format != "console" {
		t.Errorf("log format: got %q, want console", cfg.Log.Format)
	}
	if cfg.Agent.HeartbeatInterval != 60*time.Second {
		t.Errorf("heartbeat interval: got %v, want 60s", cfg.Agent.HeartbeatInterval)
	}
	if cfg.Agent.TelemetryInterval != 120*time.Second {
		t.Errorf("telemetry interval: got %v, want 120s", cfg.Agent.TelemetryInterval)
	}
}

func TestLoad_NilOrEmptyAgentSettingsUsesDefaults(t *testing.T) {
	for _, raw := range [][]byte{nil, {}, []byte("null"), []byte("  null  ")} {
		cfg, err := config.Load(raw)
		if err != nil {
			t.Fatalf("Load(%q) error: %v", raw, err)
		}
		if cfg.NATS.URL != "nats://localhost:4222" {
			t.Errorf("Load(%q): NATS URL: got %q, want default", raw, cfg.NATS.URL)
		}
	}
}

// TestLoad_FromRealisticConfigDriveSample loads pkg/isoconfig/testdata/pc-meta-data.json
// — a sanitized copy of a real config-drive payload — through the actual
// isoconfig reader, then through config.Load, as an end-to-end regression
// check for the full pc-meta-data.json -> agent JSON -> Config pipeline.
func TestLoad_FromRealisticConfigDriveSample(t *testing.T) {
	meta, err := isoconfig.NewReader("../../pkg/isoconfig/testdata").Read()
	if err != nil {
		t.Fatalf("isoconfig Read() error: %v", err)
	}

	cfg, err := config.Load(meta.AgentSettings())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.NATS.WebSocketURL != "wss://nats.example.com:443" {
		t.Errorf("NATS.WebSocketURL: got %q", cfg.NATS.WebSocketURL)
	}
	if cfg.NATS.ConnectionType != "websocket" {
		t.Errorf("NATS.ConnectionType: got %q", cfg.NATS.ConnectionType)
	}
	if cfg.Log.Level != "debug" || cfg.Log.Format != "console" {
		t.Errorf("Log: got level=%q format=%q", cfg.Log.Level, cfg.Log.Format)
	}
	if len(cfg.Agent.AllowedOperations) == 0 {
		t.Error("Agent.AllowedOperations should not be empty")
	}
	if cfg.Autoheal.RestartDelay != 10*time.Second {
		t.Errorf("Autoheal.RestartDelay: got %v, want 10s", cfg.Autoheal.RestartDelay)
	}

	// Top-level identity fields take precedence over agent.nats.* in main.go,
	// but both should be present and equal in this sample.
	if meta.VMID() != cfg.NATS.AgentUUID {
		t.Errorf("VMID %q should match merged NATS.AgentUUID %q in this sample", meta.VMID(), cfg.NATS.AgentUUID)
	}
}

// --- Environment variable overrides ---

func TestLoad_EnvVarOverridesDefault(t *testing.T) {
	t.Setenv("PLUSCLOUDS_AGENT_NATS_URL", "nats://env-server:4222")
	t.Setenv("PLUSCLOUDS_AGENT_LOG_LEVEL", "warn")

	cfg, err := config.Load(nil)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.NATS.URL != "nats://env-server:4222" {
		t.Errorf("NATS URL via env: got %q, want nats://env-server:4222", cfg.NATS.URL)
	}
	if cfg.Log.Level != "warn" {
		t.Errorf("log level via env: got %q, want warn", cfg.Log.Level)
	}
}

func TestLoad_EnvVarOverridesISOSettings(t *testing.T) {
	t.Setenv("PLUSCLOUDS_AGENT_NATS_URL", "nats://env-wins:4222")

	cfg, err := config.Load([]byte(`{"nats": {"url": "nats://iso-server:4222"}}`))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.NATS.URL != "nats://env-wins:4222" {
		t.Errorf("env should override config-drive settings: got %q, want nats://env-wins:4222", cfg.NATS.URL)
	}
}

// --- Error cases ---

func TestLoad_InvalidJSON_ReturnsError(t *testing.T) {
	_, err := config.Load([]byte(`{"nats": invalid`))
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}
