// Package notify delivers formatted session and quota notifications to a dedicated Telegram Bot.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client sends HTML messages through a dedicated Telegram Bot API endpoint.
type Client struct {
	httpClient *http.Client
	baseURL    string // empty uses https://api.telegram.org
}

// NewClient constructs a Telegram Bot notifier.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 6 * time.Second},
		baseURL:    "https://api.telegram.org",
	}
}

// SendHTML posts a Telegram HTML message to the given chat ID.
func (c *Client) SendHTML(ctx context.Context, botToken, chatID, messageHTML string) error {
	botToken = strings.TrimSpace(botToken)
	chatID = strings.TrimSpace(chatID)
	if botToken == "" || chatID == "" {
		return fmt.Errorf("telegram botToken and chatId are required")
	}
	payload := map[string]any{
		"chat_id":                  chatID,
		"text":                     messageHTML,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/bot%s/sendMessage", strings.TrimRight(c.baseURL, "/"), botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if json.Unmarshal(body, &parsed) == nil && !parsed.OK {
		return fmt.Errorf("telegram API error: %s", parsed.Description)
	}
	return nil
}

// FormatSessionStop renders a compact Telegram notification card when a task stops or errors.
func FormatSessionStop(instID, title, workspace, model, reason, errText string, durSec int64, isErr bool) string {
	icon := "✅"
	heading := "任务已完成"
	if isErr {
		icon = "🔴"
		heading = "任务异常中断"
	}
	if title == "" {
		title = "未命名会话"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s <b>AgyMate · %s</b>\n", icon, heading)
	fmt.Fprintf(&b, "实例: <code>%s</code>\n", html.EscapeString(strings.ToUpper(instID)))
	fmt.Fprintf(&b, "会话: %s\n", html.EscapeString(title))
	if workspace != "" {
		fmt.Fprintf(&b, "工作区: <code>%s</code>\n", html.EscapeString(workspace))
	}
	if model != "" {
		fmt.Fprintf(&b, "模型: <code>%s</code>\n", html.EscapeString(model))
	}
	if durSec > 0 {
		fmt.Fprintf(&b, "耗时: %ds\n", durSec)
	}
	if reason != "" {
		fmt.Fprintf(&b, "终止原因: <code>%s</code>\n", html.EscapeString(reason))
	}
	if errText != "" {
		if len(errText) > 220 {
			errText = errText[:220] + "..."
		}
		fmt.Fprintf(&b, "错误: <code>%s</code>\n", html.EscapeString(errText))
	}
	return b.String()
}

// FormatWaitingAlert renders a Telegram alert when an instance waits for user confirmation.
func FormatWaitingAlert(instID, title, workspace string, waitSec int64) string {
	if title == "" {
		title = "未命名会话"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🟡 <b>AgyMate · 等待确认</b>\n")
	fmt.Fprintf(&b, "实例: <code>%s</code>\n", html.EscapeString(strings.ToUpper(instID)))
	fmt.Fprintf(&b, "会话: %s\n", html.EscapeString(title))
	if workspace != "" {
		fmt.Fprintf(&b, "工作区: <code>%s</code>\n", html.EscapeString(workspace))
	}
	fmt.Fprintf(&b, "已等待: %ds（可能卡在命令审批或提问选项）", waitSec)
	return b.String()
}

// FormatLowQuotaAlert renders a Telegram alert when a quota bucket drops below threshold.
func FormatLowQuotaAlert(instID, groupName, window string, remainingPct float64, resetTime string) string {
	return fmt.Sprintf(
		"⚠️ <b>AgyMate · 配额预警</b>\n实例: <code>%s</code>\n配额池: %s (%s)\n剩余: <b>%.1f%%</b>\n重置时间: <code>%s</code>",
		html.EscapeString(strings.ToUpper(instID)),
		html.EscapeString(groupName),
		html.EscapeString(window),
		remainingPct,
		html.EscapeString(resetTime),
	)
}
