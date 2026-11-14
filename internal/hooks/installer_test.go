package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
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
}

func TestTrackerLifecycle(t *testing.T) {
	tr := NewTracker(func() config.Config { return config.DefaultConfig() })
	s1 := tr.Ingest(EventPayload{
		InstanceID:     "main",
		ConversationID: "cid-abc",
		HookEventName:  "PreInvocation",
		WorkspacePaths: []string{"/Users/chendahuang/code/AgyMate"},
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
