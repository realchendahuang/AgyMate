package doctor

import (
	"os"
	"testing"
)

func TestEnsureAndCheckSidecarIn(t *testing.T) {
	home := t.TempDir()
	cli := "/Users/example/.local/bin/agymate"

	ok, _ := CheckSidecarIn(home, cli)
	if ok {
		t.Fatal("expected CheckSidecarIn to be false before registration")
	}

	writtenPath, err := EnsureSidecarIn(home, cli)
	if err != nil {
		t.Fatalf("EnsureSidecarIn error: %v", err)
	}
	if _, err := os.Stat(writtenPath); err != nil {
		t.Fatalf("expected %s to exist: %v", writtenPath, err)
	}
	if _, err := os.Stat(writtenPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected no lingering .tmp file next to %s", writtenPath)
	}

	ok, detail := CheckSidecarIn(home, cli)
	if !ok {
		t.Fatalf("expected CheckSidecarIn=true after EnsureSidecarIn, got false (%s)", detail)
	}
}

func TestParseKeychainMdat(t *testing.T) {
	sample := `keychain: "/Users/chendahuang/Library/Keychains/login.keychain-db"
class: "genp"
attributes:
    "acct"<blob>="antigravity"
    "mdat"<timedate>=0x32303236313131343032343932335A00  "20261114024923Z\000"
    "svce"<blob>="gemini"`

	got := parseKeychainMdat(sample)
	if got == "" {
		t.Fatal("expected non-empty parsed timestamp from mdat sample")
	}
}
