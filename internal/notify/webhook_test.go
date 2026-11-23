package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/realchendahuang/AgyMate/internal/config"
)

func TestDetectWebhookFormat(t *testing.T) {
	tests := []struct {
		url    string
		cfg    string
		expect string
	}{
		{"https://discord.com/api/webhooks/123/abc", "auto", "discord"},
		{"https://hooks.slack.com/services/T0/B0/XX", "auto", "slack"},
		{"https://open.feishu.cn/open-apis/bot/v2/hook/abc", "auto", "feishu"},
		{"https://oapi.dingtalk.com/robot/send?access_token=abc", "auto", "dingtalk"},
		{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc", "auto", "wecom"},
		{"https://api.day.app/device-key/", "auto", "bark"},
		{"https://example.com/hook", "auto", "generic"},
		{"https://example.com/hook", "feishu", "feishu"},
	}
	for _, tc := range tests {
		if got := DetectWebhookFormat(tc.url, tc.cfg); got != tc.expect {
			t.Errorf("DetectWebhookFormat(%q, %q) = %q, want %q", tc.url, tc.cfg, got, tc.expect)
		}
	}
}

func TestPlainTextFromHTML(t *testing.T) {
	in := "⚠️ <b>AgyMate · Alert</b>\nAccount: <code>a&lt;b@example.com</code>"
	got := PlainTextFromHTML(in)
	want := "⚠️ AgyMate · Alert\nAccount: a<b@example.com"
	if got != want {
		t.Fatalf("PlainTextFromHTML() = %q, want %q", got, want)
	}
}

func TestSendWebhookGenericWithHMACAndFeishu(t *testing.T) {
	var gotBody []byte
	var gotSig string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotSig = r.Header.Get("X-AgyMate-Signature")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	cli := NewClient()
	secret := "my-webhook-secret"
	err := cli.SendWebhook(context.Background(), ts.URL, "generic", secret, "AgyMate Test", "<b>Hello</b> <code>world</code>")
	if err != nil {
		t.Fatalf("SendWebhook generic failed: %v", err)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(gotBody)
	wantSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if gotSig != wantSig {
		t.Fatalf("expected signature %q, got %q", wantSig, gotSig)
	}

	var genericPayload map[string]any
	if err := json.Unmarshal(gotBody, &genericPayload); err != nil {
		t.Fatalf("unmarshal generic payload: %v", err)
	}
	if genericPayload["text"] != "Hello world" || genericPayload["event"] != "agymate.alert" {
		t.Fatalf("unexpected generic payload: %+v", genericPayload)
	}

	// Verify Feishu payload structure
	err = cli.SendWebhook(context.Background(), ts.URL, "feishu", "", "Title", "<b>Quota Low</b>")
	if err != nil {
		t.Fatalf("SendWebhook feishu failed: %v", err)
	}
	var feishuPayload struct {
		MsgType string            `json:"msg_type"`
		Content map[string]string `json:"content"`
	}
	if err := json.Unmarshal(gotBody, &feishuPayload); err != nil {
		t.Fatalf("unmarshal feishu payload: %v", err)
	}
	if feishuPayload.MsgType != "text" || feishuPayload.Content["text"] != "Quota Low" {
		t.Fatalf("unexpected feishu payload: %+v", feishuPayload)
	}
}

func TestDispatchWebhookOnly(t *testing.T) {
	called := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "Quota Warning") {
			t.Errorf("unexpected webhook body: %s", string(body))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := config.DefaultConfig()
	cfg.Webhook.Enabled = true
	cfg.Webhook.URL = ts.URL
	cfg.Webhook.Format = "slack"

	cli := NewClient()
	if err := cli.Dispatch(context.Background(), cfg, "Quota", "<b>Quota Warning</b>"); err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}
	if !called {
		t.Fatal("expected webhook server to be invoked")
	}
}
