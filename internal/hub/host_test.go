package hub

import (
	"testing"
)

func TestIsAllowedHost(t *testing.T) {
	listenAddr := "127.0.0.1:3778"

	tests := []struct {
		host string
		want bool
	}{
		{"127.0.0.1:3778", true},
		{"localhost:3778", true},
		{"attacker.com", false},
		{"127.0.0.1:3778.attacker.com", false},
		{"localhost:3778.attacker.com", false},
	}

	for _, tt := range tests {
		got := isAllowedHost(tt.host, listenAddr)
		if got != tt.want {
			t.Errorf("isAllowedHost(%q, %q) = %v, want %v", tt.host, listenAddr, got, tt.want)
		}
	}
}
