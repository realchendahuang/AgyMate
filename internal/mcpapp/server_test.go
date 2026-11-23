package mcpapp

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realchendahuang/AgyMate/internal/config"
)

func isolateMCPEnv(t *testing.T) config.Config {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AGYMATE_STATE", filepath.Join(dir, "state.json"))
	t.Setenv("AGYMATE_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("AGYMATE_HUB_TOKEN", filepath.Join(dir, "hub.token"))
	t.Setenv("AGYMATE_COLAB_ACCOUNTS", filepath.Join(dir, "colab.json"))

	cfg := config.DefaultConfig()
	cfg.ListenAddr = "127.0.0.1:0"
	cfg.Paths.ProfilesDir = filepath.Join(dir, "profiles")
	cfg.Colab.Enabled = false
	return cfg
}

func TestMCPInitializeToolsResourcesAndGuardrails(t *testing.T) {
	cfg := isolateMCPEnv(t)
	srv := NewServer(cfg)

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{"extensions":{"io.modelcontextprotocol/ui":{"mimeTypes":["text/html;profile=mcp-app"]}}}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"ui://agymate/app.html"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"open_dashboard","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"get_status","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"app_action","arguments":{"action":"profile_stop","instance":"main"}}}`,
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"app_action","arguments":{"action":"profile_stop","instance":"ide"}}}`,
	}, "\n") + "\n"

	var out bytes.Buffer
	if err := srv.ServeStream(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatalf("ServeStream failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 8 {
		t.Fatalf("expected 8 JSON-RPC responses (notification skipped), got %d:\n%s", len(lines), out.String())
	}

	resps := make(map[int]map[string]any)
	for _, line := range lines {
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("invalid JSON response %q: %v", line, err)
		}
		id := int(obj["id"].(float64))
		resps[id] = obj
	}

	// 1. initialize advertises ui extension
	initRes := resps[1]["result"].(map[string]any)
	caps := initRes["capabilities"].(map[string]any)
	exts := caps["extensions"].(map[string]any)
	if _, ok := exts["io.modelcontextprotocol/ui"]; !ok {
		t.Fatalf("missing io.modelcontextprotocol/ui extension in initialize capabilities: %+v", caps)
	}

	// 2. tools/list has open_dashboard with aux_pane + global entrypoints and app_action with visibility=["app"]
	toolsRes := resps[2]["result"].(map[string]any)
	tools := toolsRes["tools"].([]any)
	var sawOpenDash, sawGetStatus, sawAppAction bool
	for _, rawT := range tools {
		tool := rawT.(map[string]any)
		switch tool["name"] {
		case "open_dashboard":
			sawOpenDash = true
			meta := tool["_meta"].(map[string]any)
			uiMeta := meta["ui"].(map[string]any)
			if uiMeta["resourceUri"] != AppResourceURI {
				t.Fatalf("open_dashboard resourceUri = %v, want %s", uiMeta["resourceUri"], AppResourceURI)
			}
			agyUI := meta["com.google.antigravity/ui"].(map[string]any)
			eps := agyUI["entrypoints"].([]any)
			if len(eps) != 2 {
				t.Fatalf("expected 2 entrypoints (aux_pane, global), got %+v", eps)
			}
		case "get_status":
			sawGetStatus = true
			meta := tool["_meta"].(map[string]any)
			uiMeta := meta["ui"].(map[string]any)
			if uiMeta["resourceUri"] != AppResourceURI {
				t.Fatalf("get_status resourceUri = %v, want %s", uiMeta["resourceUri"], AppResourceURI)
			}
		case "app_action":
			sawAppAction = true
			meta := tool["_meta"].(map[string]any)
			uiMeta := meta["ui"].(map[string]any)
			vis := uiMeta["visibility"].([]any)
			if len(vis) != 1 || vis[0] != "app" {
				t.Fatalf("expected app_action visibility=[\"app\"], got %+v", vis)
			}
		}
	}
	if !sawOpenDash || !sawGetStatus || !sawAppAction {
		t.Fatalf("missing expected tools: open_dashboard=%v get_status=%v app_action=%v", sawOpenDash, sawGetStatus, sawAppAction)
	}

	// 4. resources/read returns valid single-file MCP App HTML
	readRes := resps[4]["result"].(map[string]any)
	contents := readRes["contents"].([]any)
	doc := contents[0].(map[string]any)
	if doc["mimeType"] != AppResourceMimeType {
		t.Fatalf("mimeType = %v, want %s", doc["mimeType"], AppResourceMimeType)
	}
	htmlText, _ := doc["text"].(string)
	if !strings.Contains(htmlText, "<!DOCTYPE html>") || !strings.Contains(htmlText, "AgyMate") {
		t.Fatalf("unexpected HTML document returned (len=%d)", len(htmlText))
	}

	// 5. open_dashboard returns view=dashboard
	openRes := resps[5]["result"].(map[string]any)
	openSC := openRes["structuredContent"].(map[string]any)
	if openSC["view"] != "dashboard" {
		t.Fatalf("open_dashboard structuredContent.view = %v, want dashboard", openSC["view"])
	}

	// 6. get_status returns view=inline
	statusRes := resps[6]["result"].(map[string]any)
	statusSC := statusRes["structuredContent"].(map[string]any)
	if statusSC["view"] != "inline" {
		t.Fatalf("get_status structuredContent.view = %v, want inline", statusSC["view"])
	}

	// 7 & 8. main and ide are strictly protected from lifecycle mutations
	for _, id := range []int{7, 8} {
		r := resps[id]["result"].(map[string]any)
		if isErr, _ := r["isError"].(bool); !isErr {
			t.Fatalf("expected isError=true when attempting to stop protected slot (id=%d), got %+v", id, r)
		}
	}
}
