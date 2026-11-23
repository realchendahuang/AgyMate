package setup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/realchendahuang/AgyMate/internal/config"
)

func TestSetupDryRunDoesNotWriteFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgPath := filepath.Join(home, ".config", "agymate", "config.json")
	tokPath := filepath.Join(home, ".config", "agymate", "hub.token")
	t.Setenv("AGYMATE_CONFIG", cfgPath)
	t.Setenv("AGYMATE_HUB_TOKEN", tokPath)
	t.Setenv("AGYMATE_STATE", filepath.Join(home, ".local", "share", "agymate", "state.json"))

	pluginFS := fstest.MapFS{
		"plugin.json":        &fstest.MapFile{Data: []byte(`{"name":"agymate"}`)},
		"mcp_config.json":    &fstest.MapFile{Data: []byte(`{"mcpServers":{"hub":{"command":"bash","args":["${PLUGIN_ROOT}/scripts/run-mcp.sh"]}}}`)},
		"scripts/run-mcp.sh": &fstest.MapFile{Data: []byte("#!/bin/sh\nexec agymate mcp\n")},
	}

	rep := Run(context.Background(), Options{
		BinPath:             "/opt/homebrew/bin/agymate",
		DryRun:              true,
		NoMagpie:            true,
		AntigravityPluginFS: pluginFS,
		HomeDir:             home,
	})
	if !rep.OK || !rep.DryRun {
		t.Fatalf("expected OK dry-run report, got %+v", rep)
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run must not create %s", cfgPath)
	}
	if _, err := os.Stat(filepath.Join(home, ".gemini", "config", "plugins", "agymate")); !os.IsNotExist(err) {
		t.Fatal("dry-run must not extract plugin files")
	}
}

func TestSetupInstallMigrateAndCleanUninstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgPath := filepath.Join(home, ".config", "agymate", "config.json")
	tokPath := filepath.Join(home, ".config", "agymate", "hub.token")
	statePath := filepath.Join(home, ".local", "share", "agymate", "state.json")
	t.Setenv("AGYMATE_CONFIG", cfgPath)
	t.Setenv("AGYMATE_HUB_TOKEN", tokPath)
	t.Setenv("AGYMATE_STATE", statePath)

	// Seed legacy config with bottom-right HUD position and custom field
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	legacyCfg := `{"listenAddr":"127.0.0.1:4778","cdp":{"hudPosition":"bottom-right"},"pricing":{"currency":"CNY"}}`
	if err := os.WriteFile(cfgPath, []byte(legacyCfg), 0o600); err != nil {
		t.Fatal(err)
	}

	// Seed existing third-party hook to verify non-destructive merge and uninstall
	hooksPath := filepath.Join(home, ".gemini", "config", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksPath, []byte(`{"third_party":{"Stop":[]}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	pluginFS := fstest.MapFS{
		"plugin.json":        &fstest.MapFile{Data: []byte(`{"name":"agymate"}`)},
		"mcp_config.json":    &fstest.MapFile{Data: []byte(`{"mcpServers":{"hub":{"command":"bash","args":["${PLUGIN_ROOT}/scripts/run-mcp.sh"]}}}`)},
		"scripts/run-mcp.sh": &fstest.MapFile{Data: []byte("#!/bin/sh\nexec agymate mcp\n")},
	}

	rep := Run(context.Background(), Options{
		BinPath:             "/opt/homebrew/bin/agymate",
		NoService:           true,
		NoMagpie:            true,
		NoWait:              true,
		AntigravityPluginFS: pluginFS,
		HomeDir:             home,
	})
	if !rep.OK {
		t.Fatalf("expected setup OK, got %+v", rep)
	}
	if rep.ListenAddr != "127.0.0.1:4778" {
		t.Fatalf("expected ListenAddr=127.0.0.1:4778, got %q", rep.ListenAddr)
	}

	// Verify smooth migration preserved currency while upgrading hudPosition
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.CDP.HUDPosition != "top-right" || cfg.Pricing.Currency != "CNY" {
		t.Fatalf("unexpected migrated config: cdp=%+v pricing=%+v", cfg.CDP, cfg.Pricing)
	}

	// Verify plugin, hooks, and sidecar were created
	pluginManifest := filepath.Join(home, ".gemini", "config", "plugins", "agymate", "plugin.json")
	if _, err := os.Stat(pluginManifest); err != nil {
		t.Fatalf("expected installed plugin manifest: %v", err)
	}
	sidecarManifest := filepath.Join(home, ".gemini", "config", "sidecars", "agymate", "sidecar.json")
	if _, err := os.Stat(sidecarManifest); err != nil {
		t.Fatalf("expected installed sidecar manifest: %v", err)
	}

	// Verify clean uninstall keeps third_party hooks and keeps config unless --purge
	uRep := Uninstall(context.Background(), UninstallOptions{
		NoService: true,
		PurgeData: false,
		HomeDir:   home,
	})
	if !uRep.OK {
		t.Fatalf("expected uninstall OK, got %+v", uRep)
	}
	rawHooks, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("read hooks after uninstall: %v", err)
	}
	var hooksDoc map[string]any
	if err := json.Unmarshal(rawHooks, &hooksDoc); err != nil {
		t.Fatal(err)
	}
	if _, hasAgy := hooksDoc["agymate"]; hasAgy {
		t.Fatal("expected agymate hook to be removed by uninstall")
	}
	if _, hasThird := hooksDoc["third_party"]; !hasThird {
		t.Fatal("expected third_party hook to remain intact after uninstall")
	}
	if _, err := os.Stat(pluginManifest); !os.IsNotExist(err) {
		t.Fatal("expected plugin directory to be removed by uninstall")
	}
	if _, err := os.Stat(sidecarManifest); !os.IsNotExist(err) {
		t.Fatal("expected sidecar directory to be removed by uninstall")
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected config to be preserved without --purge: %v", err)
	}

	// Verify --purge removes config directory cleanly
	pRep := Uninstall(context.Background(), UninstallOptions{
		NoService: true,
		PurgeData: true,
		HomeDir:   home,
	})
	if !pRep.OK {
		t.Fatalf("expected purge uninstall OK, got %+v", pRep)
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatal("expected config file to be removed with --purge")
	}
}
