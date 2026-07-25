// Command plusclouds-agent is the PlusClouds VM agent daemon.
// It collects system metrics, manages services, and communicates with the
// PlusClouds platform exclusively via NATS. Supports Linux and Windows.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/plusclouds/ubuntu-agent/internal/config"
	"github.com/plusclouds/ubuntu-agent/internal/dispatcher"
	"github.com/plusclouds/ubuntu-agent/internal/executor"
	"github.com/plusclouds/ubuntu-agent/internal/modules/diskresize"
	"github.com/plusclouds/ubuntu-agent/internal/modules/pfsense"
	"github.com/plusclouds/ubuntu-agent/internal/modules/system"
	natsclient "github.com/plusclouds/ubuntu-agent/internal/nats"
	"github.com/plusclouds/ubuntu-agent/internal/protocol"
	"github.com/plusclouds/ubuntu-agent/internal/publisher"
	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

func main() {
	root := &cobra.Command{
		Use:     "plusclouds-agent",
		Short:   "PlusClouds VM Agent",
		Version: config.AgentVersion,
		RunE:    run,
	}

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(_ *cobra.Command, _ []string) error {
	// ------------------------------------------------------------------ //
	// 1. Bootstrap: built-in defaults only, just enough to locate the
	//    config-drive (or its local cache).
	// ------------------------------------------------------------------ //
	bootCfg, err := config.Load(nil)
	if err != nil {
		return fmt.Errorf("loading bootstrap config: %w", err)
	}
	bootLogger, err := buildLogger(bootCfg)
	if err != nil {
		return fmt.Errorf("building bootstrap logger: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bootExec := executor.New(bootLogger)

	// ------------------------------------------------------------------ //
	// 2. Locate and read the config-drive (or its local cache), then build
	//    the real configuration by merging its "agent" settings on top of
	//    defaults. There is no config file anymore — the platform writes
	//    everything (identity, NATS, allowed operations, logging, ...)
	//    into pc-meta-data.json at provisioning time.
	// ------------------------------------------------------------------ //
	locator := isoconfig.NewLocator(bootExec, bootLogger)
	iso, err := isoconfig.Load(ctx, locator, bootCfg.ISO.MountPath, bootCfg.ISO.CachePath, bootCfg.ISO.Label, bootLogger)
	if err != nil {
		bootLogger.Warn("could not resolve config-drive metadata, running on built-in defaults",
			zap.Error(err),
		)
		iso = &isoconfig.ISOMetadata{}
	}

	cfg, err := config.Load(iso.AgentSettings())
	if err != nil {
		return fmt.Errorf("building config from config-drive metadata: %w", err)
	}

	// ------------------------------------------------------------------ //
	// 3. Initialise the real logger, now that log config may have been
	//    overridden by the config-drive metadata.
	// ------------------------------------------------------------------ //
	logger, err := buildLogger(cfg)
	if err != nil {
		return fmt.Errorf("building logger: %w", err)
	}
	defer logger.Sync() //nolint:errcheck

	logger.Info("PlusClouds agent starting",
		zap.String("version", config.AgentVersion),
	)

	exec := executor.New(logger)

	// ------------------------------------------------------------------ //
	// 4. Resolve identity. The top-level config-drive identity fields
	//    (virtual_machine_id, agent_api_key) always take precedence over
	//    whatever the merged agent.nats settings say.
	// ------------------------------------------------------------------ //
	agentUUID := cfg.NATS.AgentUUID
	agentAPIKey := cfg.NATS.APIKey

	if iso.VMID() != "" {
		agentUUID = iso.VMID()
		agentAPIKey = iso.AgentAPIKey()
	}

	if agentUUID == "" {
		logger.Warn("agent_uuid is not set — NATS auth will fail; check the config-drive metadata")
	}
	if agentAPIKey == "" {
		logger.Warn("api_key is not set — NATS auth will fail; check the config-drive metadata")
	}

	logger.Info("agent identity resolved", zap.String("agent_uuid", agentUUID))

	// ------------------------------------------------------------------ //
	// 5. Initialise platform-specific service manager
	// ------------------------------------------------------------------ //
	svcMgr, svcCleanup := newServiceManager(ctx, logger)
	defer svcCleanup()

	// ------------------------------------------------------------------ //
	// 6. Initialise remaining modules
	// ------------------------------------------------------------------ //
	sysMod := system.New(iso)
	pfsMod := pfsense.New(exec, logger)
	resizer := diskresize.New(exec, logger)
	logger.Info("modules initialised",
		zap.Int("allowed_operations", len(cfg.Agent.AllowedOperations)),
		zap.Int("allowed_commands", len(cfg.Agent.AllowedCommands)),
		zap.Duration("telemetry_interval", cfg.Agent.TelemetryInterval),
		zap.Duration("heartbeat_interval", cfg.Agent.HeartbeatInterval),
	)

	// ------------------------------------------------------------------ //
	// 6b. One-shot boot provisioning from ISO metadata (pfSense only).
	// Applies static network config and the provisioned password at most
	// once; failures are logged and retried on the next boot, never fatal.
	// ------------------------------------------------------------------ //
	if runtime.GOOS == "freebsd" {
		runBootProvisioning(ctx, cfg, iso, agentUUID, agentAPIKey, pfsMod, logger)
	}

	// ------------------------------------------------------------------ //
	// 7. Connect to NATS
	// ------------------------------------------------------------------ //
	nc, err := natsclient.Connect(cfg.NATS, agentUUID, agentAPIKey, logger)
	if err != nil {
		return fmt.Errorf("NATS connection failed: %w", err)
	}
	defer nc.Drain()

	// ------------------------------------------------------------------ //
	// 8. Create publisher and dispatcher
	// ------------------------------------------------------------------ //
	pub := publisher.New(nc, sysMod, agentUUID, cfg.Agent, logger)

	disp := dispatcher.New(
		sysMod, svcMgr, pfsMod, exec, resizer, pub,
		agentUUID,
		cfg.Agent.AllowedOperations,
		cfg.Agent.AllowedCommands,
		logger,
	)

	// ------------------------------------------------------------------ //
	// 9. Subscribe to cmd subject
	// ------------------------------------------------------------------ //
	if err := nc.Subscribe(func(env protocol.Envelope) {
		result := disp.Dispatch(ctx, env)

		if env.ReplyTo != "" {
			// Synchronous caller is blocking on this inbox — reply directly.
			if err := nc.PublishToSubject(env.ReplyTo, result); err != nil {
				logger.Error("could not publish sync result",
					zap.String("command_id", env.ID),
					zap.String("reply_to", env.ReplyTo),
					zap.Error(err),
				)
			}
		} else {
			// Async path — result goes to the evt subject as usual.
			if err := nc.Publish(result); err != nil {
				logger.Error("could not publish result to evt subject",
					zap.String("command_id", env.ID),
					zap.Error(err),
				)
			}
		}
	}); err != nil {
		return fmt.Errorf("subscribing to NATS cmd subject: %w", err)
	}

	// ------------------------------------------------------------------ //
	// 10. Start heartbeat and telemetry publisher
	// ------------------------------------------------------------------ //
	pub.Start(ctx)

	logger.Info("agent started",
		zap.String("nats_url", cfg.NATS.ActiveURL()),
		zap.String("cmd_subject", nc.CmdSubject()),
		zap.String("evt_subject", nc.EvtSubject()),
	)

	// ------------------------------------------------------------------ //
	// 11. Wait for OS signal
	// ------------------------------------------------------------------ //
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	logger.Info("received shutdown signal", zap.String("signal", sig.String()))

	logger.Info("initiating graceful shutdown")
	cancel()
	logger.Info("agent stopped cleanly")
	return nil
}

// netconfigMarkerPath and passwordMarkerPath mark completed one-shot boot
// provisioning steps. /var/db is persistent storage on a Full-Install
// pfSense (NanoBSD is no longer supported as of pfSense 2.6+).
const (
	netconfigMarkerPath = "/var/db/plusclouds-agent/netconfig.applied"
	passwordMarkerPath  = "/var/db/plusclouds-agent/password-set.applied"
)

// runBootProvisioning applies one-shot, metadata-driven network and password
// provisioning. The two steps are independent: one failing/reverting must
// not block the other, and each retries on the next boot until it succeeds.
func runBootProvisioning(ctx context.Context, cfg *config.Config, iso *isoconfig.ISOMetadata, agentUUID, agentAPIKey string, pfsMod pfsense.Manager, logger *zap.Logger) {
	applyBootNetworkConfig(ctx, cfg, iso, agentUUID, agentAPIKey, pfsMod, logger)
	applyBootPassword(ctx, iso, pfsMod, logger)
}

// applyBootNetworkConfig sets static addressing on already-assigned pfSense
// interfaces from the ISO metadata, then verifies the change by attempting a
// real (short, non-retrying) NATS connect — if the platform can't be reached
// afterward, it reverts to the pre-change snapshot so the box stays
// reachable and retries on the next boot. Only a verified-successful apply
// is marked done.
func applyBootNetworkConfig(ctx context.Context, cfg *config.Config, iso *isoconfig.ISOMetadata, agentUUID, agentAPIKey string, pfsMod pfsense.Manager, logger *zap.Logger) {
	if _, err := os.Stat(netconfigMarkerPath); err == nil {
		return
	}

	cards := iso.NetworkCards()
	if len(cards) == 0 {
		return
	}

	result, err := pfsMod.ApplyBootNetworkConfig(ctx, cards)
	if err != nil {
		logger.Error("boot network config: apply failed", zap.Error(err))
		return
	}
	if result == nil {
		return
	}
	for _, r := range result.Interfaces {
		logger.Info("boot network config: interface result",
			zap.String("mac_addr", r.MACAddr),
			zap.String("ifname", r.IfName),
			zap.String("logical", r.Logical),
			zap.Bool("applied", r.Applied),
			zap.Bool("assigned", r.Assigned),
			zap.String("message", r.Message),
		)
	}

	// Verification probe: a real NATS connect with no reconnect retries, on
	// whatever network config is now live. Not the normal, infinite-retry
	// connect used in step 6 — this one is only here to prove reachability.
	probeCfg := cfg.NATS
	probeCfg.MaxReconnects = 0
	nc, probeErr := natsclient.Connect(probeCfg, agentUUID, agentAPIKey, logger)
	if probeErr == nil {
		nc.Drain()
	}

	if probeErr != nil {
		logger.Warn("boot network config: platform unreachable after apply — reverting",
			zap.Error(probeErr))
		if len(result.Snapshot) == 0 {
			logger.Error("boot network config: no snapshot to revert to")
			return
		}
		if err := pfsMod.Revert(ctx, result.Snapshot); err != nil {
			logger.Error("boot network config: revert failed", zap.Error(err))
			return
		}
		logger.Warn("boot network config: reverted to pre-boot config, will retry next boot")
		return
	}

	if err := writeMarker(netconfigMarkerPath); err != nil {
		logger.Error("boot network config: could not write marker file", zap.Error(err))
		return
	}
	logger.Info("boot network config: applied and verified")
}

// applyBootPassword sets the password of pfSense's default superuser
// account (matched by uid 0, not by name — see SetDefaultUserPassword)
// from the ISO metadata's password field. The metadata's username field
// (e.g. "root", a generic cross-platform convention) is not used: pfSense's
// default account is "admin" and can be renamed, so it isn't a reliable
// name to match against. Unlike network config this carries no
// NATS-reachability risk (a local OS/config change, not a network change),
// so it's a simple idempotent retry-until-success with no verification
// probe or revert.
func applyBootPassword(ctx context.Context, iso *isoconfig.ISOMetadata, pfsMod pfsense.Manager, logger *zap.Logger) {
	if _, err := os.Stat(passwordMarkerPath); err == nil {
		return
	}

	password := iso.Password()
	if password == "" {
		return
	}

	result, err := pfsMod.SetDefaultUserPassword(ctx, password)
	if err != nil {
		logger.Error("boot password provisioning: failed", zap.Error(err))
		return
	}
	if !result.Success {
		logger.Warn("boot password provisioning: not applied, will retry next boot",
			zap.String("message", result.Message),
		)
		return
	}

	if err := writeMarker(passwordMarkerPath); err != nil {
		logger.Error("boot password provisioning: could not write marker file", zap.Error(err))
		return
	}
	logger.Info("boot password provisioning: applied", zap.String("username", result.Username))
}

func writeMarker(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, nil, 0600)
}

func buildLogger(cfg *config.Config) (*zap.Logger, error) {
	var level zapcore.Level
	if err := level.UnmarshalText([]byte(cfg.Log.Level)); err != nil {
		level = zapcore.InfoLevel
	}

	encCfg := zap.NewProductionEncoderConfig()
	encCfg.TimeKey = "ts"
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	var stdoutEnc zapcore.Encoder
	if cfg.Log.Format == "console" {
		consoleEncCfg := zap.NewDevelopmentEncoderConfig()
		consoleEncCfg.EncodeTime = zapcore.ISO8601TimeEncoder
		stdoutEnc = zapcore.NewConsoleEncoder(consoleEncCfg)
	} else {
		stdoutEnc = zapcore.NewJSONEncoder(encCfg)
	}
	stdoutCore := zapcore.NewCore(stdoutEnc, zapcore.AddSync(os.Stdout), level)
	cores := []zapcore.Core{stdoutCore}

	if cfg.Log.File != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.Log.File), 0755); err != nil {
			return nil, fmt.Errorf("creating log directory: %w", err)
		}
		f, err := os.OpenFile(cfg.Log.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("opening log file %s: %w", cfg.Log.File, err)
		}
		fileCore := zapcore.NewCore(zapcore.NewJSONEncoder(encCfg), zapcore.AddSync(f), level)
		cores = append(cores, fileCore)
	}

	return zap.New(zapcore.NewTee(cores...), zap.AddCaller()), nil
}
