package hooks

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realchendahuang/AgyMate/internal/config"
)

func TestMergeHooksFilePreservesExistingKeysAndNoBackup(t *testing.T) {
	dir := t.TempDir()
	hooksPath := filepath.Join(dir, "hooks.json")

	existing := map[string]any{
		"herdr": map[string]any{
			"PreInvocation": []any{map[string]any{"type": "command", "command": "herdr-state"}},
		},
		"marktoast": map[string]any{
			"Stop": []any{map[string]any{"type": "command", "command": "marktoast hook"}},
		},
	}
	raw, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(hooksPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	spec := buildHookSpec("/usr/local/bin/agymate")
	if err := mergeHooksFile(hooksPath, spec); err != nil {
		t.Fatal(err)
	}

	// Ensure no backup files were created
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 || ents[0].Name() != "hooks.json" {
		t.Fatalf("unexpected files in dir (expected only hooks.json): %v", ents)
	}

	mergedRaw, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	var merged map[string]any
	if err := json.Unmarshal(mergedRaw, &merged); err != nil {
		t.Fatal(err)
	}
	if _, ok := merged["herdr"]; !ok {
		t.Fatal("lost herdr key")
	}
	if _, ok := merged["marktoast"]; !ok {
		t.Fatal("lost marktoast key")
	}
	agy, ok := merged["agymate"].(map[string]any)
	if !ok {
		t.Fatal("missing agymate key")
	}
	if _, hasPreTool := agy["PreToolUse"]; hasPreTool {
		t.Fatal("must not register PreToolUse hook")
	}
}

func TestInferInstanceID(t *testing.T) {
	if got := InferInstanceID(EventPayload{TranscriptPath: "/Users/x/.antigravity-profiles/p2/home/.gemini/antigravity/transcript.jsonl"}); got != "p2" {
		t.Fatalf("expected p2, got %q", got)
	}
	if got := InferInstanceID(EventPayload{TranscriptPath: "/Users/x/.gemini/antigravity-ide/transcript.jsonl"}); got != "ide" {
		t.Fatalf("expected ide, got %q", got)
	}
	if got := InferInstanceID(EventPayload{TranscriptPath: "/Users/x/.gemini/antigravity/transcript.jsonl"}); got != "main" {
		t.Fatalf("expected main, got %q", got)
	}
	if got := InferInstanceIDForDir("/srv/agy-profiles", EventPayload{TranscriptPath: "/srv/agy-profiles/client-a/home/.gemini/antigravity/transcript.jsonl"}); got != "client-a" {
		t.Fatalf("expected client-a for custom profilesDir, got %q", got)
	}
}

func TestInstallTargetsCustomProfilesDirWithoutMeta(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	profilesDir := filepath.Join(home, "custom-profiles")
	if err := os.MkdirAll(filepath.Join(profilesDir, "team-b", "data"), 0o700); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.Paths.ProfilesDir = profilesDir
	targets := InstallTargets(cfg)
	if len(targets) != 2 {
		t.Fatalf("expected 2 hook targets (global + team-b), got %v", targets)
	}
	wantProfileTarget := filepath.Join(profilesDir, "team-b", "home", ".gemini", "config", "hooks.json")
	if targets[1] != wantProfileTarget {
		t.Fatalf("expected second target %s, got %s", wantProfileTarget, targets[1])
	}
}

func TestTrackerLifecycle(t *testing.T) {
	tr := NewTracker(func() config.Config { return config.DefaultConfig() })
	s1 := tr.Ingest(EventPayload{
		InstanceID:     "main",
		ConversationID: "cid-abc",
		HookEventName:  "PreInvocation",
		WorkspacePaths: []string{"/workspace/AgyMate"},
		ModelName:      "Gemini 4 Carbon",
		LastUserInput:  "Run unit tests",
	})
	if s1.State != "running" || s1.Workspace != "AgyMate" {
		t.Fatalf("unexpected start state: %+v", s1)
	}

	s2 := tr.Ingest(EventPayload{
		InstanceID:        "main",
		ConversationID:    "cid-abc",
		HookEventName:     "Stop",
		TerminationReason: "ERROR",
		Error:             "exit status 1",
	})
	if s2.State != "error" {
		t.Fatalf("expected error state, got %+v", s2)
	}
}

func TestIngestFromStdinSendsHubToken(t *testing.T) {
	dir := t.TempDir()
	tokPath := filepath.Join(dir, "hub.token")
	t.Setenv("AGYMATE_HUB_TOKEN", tokPath)
	t.Setenv("AGYMATE_CONFIG", filepath.Join(dir, "config.json"))
	if err := os.WriteFile(tokPath, []byte("secret-hook-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var gotToken string
	var gotEvent string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-AgyMate-Token")
		var ev EventPayload
		_ = json.NewDecoder(r.Body).Decode(&ev)
		gotEvent = ev.HookEventName
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	origStdin := os.Stdin
	rPipe, wPipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { os.Stdin = origStdin }()
	os.Stdin = rPipe
	_, _ = wPipe.WriteString(`{"conversationId":"cid-hook-1","instanceId":"main"}`)
	_ = wPipe.Close()

	listenAddr := strings.TrimPrefix(ts.URL, "http://")
	out := IngestFromStdin(listenAddr, "SessionStart")
	_ = rPipe.Close()

	if strings.TrimSpace(string(out)) != "{}" {
		t.Fatalf("expected {} response, got %q", string(out))
	}
	if gotToken != "secret-hook-token" {
		t.Fatalf("expected X-AgyMate-Token=secret-hook-token, got %q", gotToken)
	}
	if gotEvent != "SessionStart" {
		t.Fatalf("expected HookEventName=SessionStart, got %q", gotEvent)
	}
}
