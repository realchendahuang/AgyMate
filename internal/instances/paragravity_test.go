package instances

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractAccountInfoV229JSON(t *testing.T) {
	dir := t.TempDir()
	gemDir := filepath.Join(dir, "home", ".gemini")
	if err := os.MkdirAll(gemDir, 0o700); err != nil {
		t.Fatal(err)
	}
	tok := map[string]any{
		"auth_method":    "oauth",
		"id_token_email": "box1@example.com",
		"token": map[string]any{
			"access_token":  "ya29.short",
			"refresh_token": "1//persistent",
			"expiry":        time.Now().Add(-48 * time.Hour).Format(time.RFC3339), // short-lived access token already expired
		},
	}
	b, _ := json.Marshal(tok)
	if err := os.WriteFile(filepath.Join(gemDir, "jetski-standalone-oauth-token"), b, 0o600); err != nil {
		t.Fatal(err)
	}

	info := ExtractAccountInfo(dir)
	if info.Email != "box1@example.com" {
		t.Fatalf("expected email box1@example.com, got %q", info.Email)
	}
	if !info.HasToken || !info.HasRefreshToken {
		t.Fatalf("expected HasToken=true and HasRefreshToken=true, got %+v", info)
	}
}

func TestExtractAccountInfoLegacyJWT(t *testing.T) {
	dir := t.TempDir()
	gemDir := filepath.Join(dir, "home", ".gemini")
	_ = os.MkdirAll(gemDir, 0o700)

	claims, _ := json.Marshal(map[string]any{
		"email": "legacy@example.com",
		"exp":   time.Now().Add(2 * time.Hour).Unix(),
	})
	jwt := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`)) + "." +
		base64.RawURLEncoding.EncodeToString(claims) + ".sig"
	_ = os.WriteFile(filepath.Join(gemDir, "jetski-standalone-oauth-token"), []byte(jwt), 0o600)

	info := ExtractAccountInfo(dir)
	if info.Email != "legacy@example.com" || !info.HasToken || info.HasRefreshToken {
		t.Fatalf("unexpected legacy info: %+v", info)
	}
}

func TestValidateProfileNameBlocksMainAndIDE(t *testing.T) {
	for _, bad := range []string{"main", "ide", "../escape", "a b", ""} {
		if err := ValidateProfileName(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
	for _, good := range []string{"p1", "p2", "work-01", "dev.box"} {
		if err := ValidateProfileName(good); err != nil {
			t.Fatalf("expected valid for %q: %v", good, err)
		}
	}
}

func TestHealHomeSymlinksRepairsBrokenTarget(t *testing.T) {
	realHome := t.TempDir()
	profHome := t.TempDir()

	// Create real target file and broken symlink pointing elsewhere
	realZsh := filepath.Join(realHome, ".zshrc")
	_ = os.WriteFile(realZsh, []byte("# zsh\n"), 0o600)
	brokenDst := filepath.Join(profHome, ".zshrc")
	_ = os.Symlink("/Users/deleted-user/.zshrc", brokenDst)

	danglingDst := filepath.Join(profHome, ".nonexistent")
	_ = os.Symlink("/Users/deleted-user/.nonexistent", danglingDst)

	repaired, removed := healHomeSymlinks(realHome, profHome, "none")
	if len(repaired) != 1 || repaired[0] != ".zshrc" {
		t.Fatalf("expected [.zshrc] repaired, got %v", repaired)
	}
	if len(removed) != 1 || removed[0] != ".nonexistent" {
		t.Fatalf("expected [.nonexistent] removed, got %v", removed)
	}
	target, err := os.Readlink(brokenDst)
	if err != nil || target != realZsh {
		t.Fatalf("expected repaired symlink -> %s, got %s (%v)", realZsh, target, err)
	}
}
