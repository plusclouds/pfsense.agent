package dispatcher

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/plusclouds/ubuntu-agent/internal/executor"
	"github.com/plusclouds/ubuntu-agent/internal/modules/pfsense"
	"github.com/plusclouds/ubuntu-agent/internal/modules/services"
	"github.com/plusclouds/ubuntu-agent/internal/modules/system"
	"github.com/plusclouds/ubuntu-agent/internal/protocol"
	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

// fakePfsense records the last SetPassword call and returns a canned result.
type fakePfsense struct {
	gotUsername string
	gotPassword string
}

func (f *fakePfsense) SetPassword(_ context.Context, username, password string) (*pfsense.SetPasswordResult, error) {
	f.gotUsername = username
	f.gotPassword = password
	return &pfsense.SetPasswordResult{Username: username, Success: true, Message: "password updated"}, nil
}

func (f *fakePfsense) ApplyBootNetworkConfig(_ context.Context, _ []isoconfig.VirtualNetworkCard) (*pfsense.BootNetworkResult, error) {
	return nil, nil
}

func (f *fakePfsense) Revert(_ context.Context, _ json.RawMessage) error {
	return nil
}

// noopServices is a services.Manager that panics if used — the redaction
// test below never exercises service operations.
type noopServices struct{}

func (noopServices) List(context.Context) ([]services.ServiceInfo, error)            { return nil, nil }
func (noopServices) Get(context.Context, string) (*services.ServiceInfo, error)      { return nil, nil }
func (noopServices) Start(context.Context, string) (*services.ActionResult, error)   { return nil, nil }
func (noopServices) Stop(context.Context, string) (*services.ActionResult, error)    { return nil, nil }
func (noopServices) Restart(context.Context, string) (*services.ActionResult, error) { return nil, nil }
func (noopServices) Reload(context.Context, string) (*services.ActionResult, error)  { return nil, nil }
func (noopServices) Enable(context.Context, string) (*services.ActionResult, error)  { return nil, nil }
func (noopServices) Disable(context.Context, string) (*services.ActionResult, error) { return nil, nil }

func TestDispatch_SetPasswordRedactsParamsInLog(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	pfs := &fakePfsense{}
	disp := New(
		system.New(isoconfig.New(nil)),
		noopServices{},
		pfs,
		executor.New(logger),
		nil, // publisher not exercised by this operation
		"agent-uuid",
		[]string{"pfsense.set_password"},
		nil,
		logger,
	)

	env, err := protocol.New("agent-uuid", protocol.TypeCommand, protocol.CommandPayload{
		Operation: "pfsense.set_password",
		Params:    json.RawMessage(`{"username":"admin","password":"super-secret-value"}`),
	})
	if err != nil {
		t.Fatalf("building command envelope: %v", err)
	}

	result := disp.Dispatch(context.Background(), env)

	var payload protocol.ResultPayload
	if err := result.DecodePayload(&payload); err != nil {
		t.Fatalf("decoding result payload: %v", err)
	}
	if payload.Status != protocol.StatusCompleted {
		t.Fatalf("expected completed status, got %q (message=%q)", payload.Status, payload.Message)
	}
	if pfs.gotUsername != "admin" || pfs.gotPassword != "super-secret-value" {
		t.Fatalf("SetPassword called with wrong args: username=%q password=%q", pfs.gotUsername, pfs.gotPassword)
	}

	for _, entry := range logs.All() {
		if strings.Contains(entry.Message, "super-secret-value") {
			t.Fatalf("log message leaked password: %q", entry.Message)
		}
		for _, f := range entry.Context {
			if strings.Contains(f.String, "super-secret-value") {
				t.Fatalf("log field %q leaked password", f.Key)
			}
		}
	}
}
