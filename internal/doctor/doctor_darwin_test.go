//go:build darwin

package doctor

import "testing"

func TestParseLaunchServicesHandlerJSON(t *testing.T) {
	sample := []byte(`{
  "LSHandlers": [
    {"LSHandlerURLScheme": "https", "LSHandlerRoleAll": "com.apple.Safari"},
    {"LSHandlerURLScheme": "antigravity", "LSHandlerRoleAll": "com.other.app"},
    {"LSHandlerURLScheme": "antigravity", "LSHandlerRoleAll": "com.google.antigravity"}
  ]
}`)
	got := parseLaunchServicesHandlerJSON(sample, "antigravity")
	if got != "com.google.antigravity" {
		t.Fatalf("expected com.google.antigravity, got %q", got)
	}
	if empty := parseLaunchServicesHandlerJSON(sample, "nonexistent"); empty != "" {
		t.Fatalf("expected empty for unknown scheme, got %q", empty)
	}
}

func TestParseKeychainMdat(t *testing.T) {
	sample := `keychain: "/Users/example/Library/Keychains/login.keychain-db"
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
