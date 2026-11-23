package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/realchendahuang/AgyMate/internal/config"
)

func TestUpdateZhViaHubPreservesWindowHUDFields(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	tokPath := filepath.Join(dir, "hub.token")
	t.Setenv("AGYMATE_CONFIG", cfgPath)
	t.Setenv("AGYMATE_HUB_TOKEN", tokPath)

	var received config.ConfigUpdate
	mockHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/config" || r.Method != http.MethodPut {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer mockHub.Close()

	u, err := url.Parse(mockHub.URL)
	if err != nil {
		t.Fatal(err)
	}

	initial := config.DefaultConfig()
	initial.ListenAddr = u.Host
	initial.CDP.Enabled = true
	initial.CDP.WindowHUD = true
	initial.CDP.HUDPosition = "bottom-left"
	initial.CDP.HUDInstances = []string{"main", "work"}
	initial.CDP.ZhInject = false
	initial.CDP.ZhScriptPath = "/tmp/custom.js"
	if err := config.Save(initial); err != nil {
		t.Fatal(err)
	}

	empty := ""
	synced, src, err := updateZhViaHubOrConfig(true, &empty)
	if err != nil {
		t.Fatalf("updateZhViaHubOrConfig error: %v", err)
	}
	if !synced {
		t.Fatalf("expected synced=true against mock hub")
	}
	if src != "builtin" {
		t.Fatalf("expected zhSource=builtin, got %q", src)
	}
	if received.CDP == nil {
		t.Fatalf("expected non-nil CDP update")
	}
	if !received.CDP.WindowHUD {
		t.Fatalf("expected WindowHUD=true to be preserved")
	}
	if received.CDP.HUDPosition != "bottom-left" {
		t.Fatalf("expected HUDPosition=bottom-left to be preserved, got %q", received.CDP.HUDPosition)
	}
	if len(received.CDP.HUDInstances) != 2 || received.CDP.HUDInstances[0] != "main" {
		t.Fatalf("expected HUDInstances to be preserved, got %v", received.CDP.HUDInstances)
	}
	if !received.CDP.ZhInject || received.CDP.ZhScriptPath != "" {
		t.Fatalf("expected ZhInject=true and ZhScriptPath=\"\", got %+v", received.CDP)
	}
}
