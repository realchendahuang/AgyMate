package main

import (
	"testing"

	"github.com/realchendahuang/AgyMate/internal/config"
)

func TestApplyConfigKeyValue(t *testing.T) {
	cfg := config.DefaultConfig()
	if err := applyConfigKeyValue(&cfg, "pricing.currency", "cny"); err != nil {
		t.Fatal(err)
	}
	if cfg.Pricing.Currency != "CNY" {
		t.Fatalf("expected CNY, got %q", cfg.Pricing.Currency)
	}
	if err := applyConfigKeyValue(&cfg, "webhook.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if err := applyConfigKeyValue(&cfg, "webhook.url", "https://hooks.slack.com/services/T/B/X"); err != nil {
		t.Fatal(err)
	}
	if !cfg.Webhook.Enabled || cfg.Webhook.URL == "" {
		t.Fatalf("expected webhook enabled, got %+v", cfg.Webhook)
	}
	if err := applyConfigKeyValue(&cfg, "unknown.key", "123"); err == nil {
		t.Fatal("expected error for unknown config key")
	}
}

func TestRedactText(t *testing.T) {
	got := redactText("home=/Users/alice/.config email=alice.dev@example.com", "/Users/alice")
	want := "home=~/.config email=a***@example.com"
	if got != want {
		t.Fatalf("redactText() = %q, want %q", got, want)
	}
}
