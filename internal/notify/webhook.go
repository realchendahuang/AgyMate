package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/realchendahuang/AgyMate/internal/config"
)

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

// PlainTextFromHTML strips Telegram-style HTML tags into clean plain text for webhook targets.
func PlainTextFromHTML(messageHTML string) string {
	stripped := htmlTagRe.ReplaceAllString(messageHTML, "")
	return strings.TrimSpace(html.UnescapeString(stripped))
}

// DetectWebhookFormat resolves "auto" into a concrete provider format based on webhook URL host.
func DetectWebhookFormat(rawURL, configured string) string {
	fmtName := config.NormalizeWebhookFormat(configured)
	if fmtName != "auto" {
		return fmtName
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "generic"
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case strings.Contains(host, "discord.com") || strings.Contains(host, "discordapp.com"):
		return "discord"
	case strings.Contains(host, "hooks.slack.com"):
		return "slack"
	case strings.Contains(host, "feishu.cn") || strings.Contains(host, "larksuite.com"):
		return "feishu"
	case strings.Contains(host, "oapi.dingtalk.com"):
		return "dingtalk"
	case strings.Contains(host, "qyapi.weixin.qq.com"):
		return "wecom"
	case strings.Contains(host, "api.day.app"):
		return "bark"
	default:
		return "generic"
	}
}

// SendWebhook posts a notification payload formatted for generic or mainstream webhook providers.
func (c *Client) SendWebhook(ctx context.Context, webhookURL, format, secret, title, messageHTML string) error {
	webhookURL = strings.TrimSpace(webhookURL)
	if webhookURL == "" {
		return fmt.Errorf("请先配置 Webhook URL")
	}
	u, err := url.Parse(webhookURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("Webhook URL 格式无效（仅支持 http:// 或 https://）")
	}

	plain := PlainTextFromHTML(messageHTML)
	if strings.TrimSpace(title) == "" {
		if firstLine, _, _ := strings.Cut(plain, "\n"); firstLine != "" {
			title = firstLine
		} else {
			title = "AgyMate Alert"
		}
	}

	provider := DetectWebhookFormat(webhookURL, format)
	var payload any
	switch provider {
	case "discord":
		payload = map[string]any{"content": plain}
	case "slack":
		payload = map[string]any{"text": plain}
	case "feishu":
		payload = map[string]any{
			"msg_type": "text",
			"content":  map[string]string{"text": plain},
		}
	case "dingtalk", "wecom":
		payload = map[string]any{
			"msgtype": "text",
			"text":    map[string]string{"content": plain},
		}
	case "bark":
		payload = map[string]any{
			"title": title,
			"body":  plain,
			"group": "AgyMate",
		}
	default:
		payload = map[string]any{
			"event":     "agymate.alert",
			"title":     title,
			"text":      plain,
			"html":      messageHTML,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "AgyMate-Notifier/1.0")
	if sec := strings.TrimSpace(secret); sec != "" {
		mac := hmac.New(sha256.New, []byte(sec))
		_, _ = mac.Write(raw)
		req.Header.Set("X-AgyMate-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Webhook (%s) 返回 HTTP %d: %s", provider, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// Dispatch delivers messageHTML to all enabled notification channels (Telegram and/or Webhook).
func (c *Client) Dispatch(ctx context.Context, cfg config.Config, title, messageHTML string) error {
	var errs []error
	if cfg.Telegram.Enabled && strings.TrimSpace(cfg.Telegram.BotToken) != "" && strings.TrimSpace(cfg.Telegram.ChatID) != "" {
		if err := c.SendHTML(ctx, cfg.Telegram.BotToken, cfg.Telegram.ChatID, messageHTML); err != nil {
			errs = append(errs, err)
		}
	}
	if cfg.Webhook.Enabled && strings.TrimSpace(cfg.Webhook.URL) != "" {
		if err := c.SendWebhook(ctx, cfg.Webhook.URL, cfg.Webhook.Format, cfg.Webhook.Secret, title, messageHTML); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
