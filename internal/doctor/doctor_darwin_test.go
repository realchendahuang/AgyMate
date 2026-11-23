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
