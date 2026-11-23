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
		return fmt.Errorf("请先配置 Telegram Bot Token 与 Chat ID")
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
		return fmt.Errorf("Telegram 接口返回 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if json.Unmarshal(body, &parsed) == nil && !parsed.OK {
		return fmt.Errorf("Telegram 接口错误: %s", parsed.Description)
	}
	return nil
}

func formatDurationCN(sec int64) string {
	if sec <= 0 {
		return "不足 1 秒"
	}
	if sec < 60 {
		return fmt.Sprintf("%d 秒", sec)
	}
	mins := sec / 60
	remSec := sec % 60
	if mins < 60 {
		if remSec == 0 {
			return fmt.Sprintf("%d 分钟", mins)
		}
		return fmt.Sprintf("%d 分 %02d 秒", mins, remSec)
	}
	hrs := mins / 60
	remMin := mins % 60
	if remMin == 0 {
		return fmt.Sprintf("%d 小时", hrs)
	}
	return fmt.Sprintf("%d 小时 %d 分", hrs, remMin)
}

func localizeTerminationReason(reason string) string {
	r := strings.TrimSpace(reason)
	switch r {
	case "", "NO_TOOL_CALL", "STOP_REASON_END_TURN":
		return ""
	case "USER_CANCELED":
		return "用户手动中止"
	case "EXCEEDED_MAX_STEPS", "MAX_STEPS_EXCEEDED":
		return "达到最大执行步数上限"
	case "MODEL_ERROR":
		return "模型调用异常"
	case "CONTEXT_LENGTH_EXCEEDED":
		return "上下文长度超出上限"
	case "RATE_LIMITED", "QUOTA_EXCEEDED":
		return "触发配额或速率限制"
	default:
		return r
	}
}

func localizeWindowName(win string) string {
	switch strings.ToLower(strings.TrimSpace(win)) {
	case "5h", "5hr", "five_hour":
		return "5小时"
	case "weekly", "7d", "1w":
		return "周配额"
	case "daily", "24h", "1d":
		return "日配额"
	case "monthly", "30d":
		return "月配额"
	default:
		return strings.TrimSpace(win)
	}
}

func formatResetTimeCN(resetTime string) string {
	raw := strings.TrimSpace(resetTime)
	if raw == "" {
		return "未知"
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t, err = time.Parse(time.RFC3339, raw)
	}
	if err != nil {
		return raw
	}
	localStr := t.Local().Format("01-02 15:04")
	diff := int64(time.Until(t).Seconds())
	if diff <= 0 {
		return localStr + "（即将重置）"
	}
	mins := diff / 60
	hrs := mins / 60
	days := hrs / 24
	switch {
	case days > 0:
		return fmt.Sprintf("%s（约 %d天%d小时后）", localStr, days, hrs%24)
	case hrs > 0:
		return fmt.Sprintf("%s（约 %d小时%d分后）", localStr, hrs, mins%60)
	default:
		if mins < 1 {
			mins = 1
		}
		return fmt.Sprintf("%s（约 %d分钟后）", localStr, mins)
	}
}

// FormatSessionStop renders a compact Telegram notification card when a task stops or errors.
func FormatSessionStop(instID, title, workspace, model, reason, errText string, durSec int64, isErr bool) string {
	icon := "✅"
	heading := "任务已完成"
	if isErr {
		icon = "🔴"
		heading = "任务异常中断"
	}
	if strings.TrimSpace(title) == "" {
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
		fmt.Fprintf(&b, "耗时: %s\n", formatDurationCN(durSec))
	}
	if reasonCN := localizeTerminationReason(reason); reasonCN != "" {
		fmt.Fprintf(&b, "结束原因: <code>%s</code>\n", html.EscapeString(reasonCN))
	}
	if errText != "" {
		runes := []rune(strings.TrimSpace(errText))
		if len(runes) > 180 {
			errText = string(runes[:180]) + "…"
		} else {
			errText = string(runes)
		}
		fmt.Fprintf(&b, "错误详情: <code>%s</code>\n", html.EscapeString(errText))
	}
	return b.String()
}

// FormatWaitingAlert renders a Telegram alert when an instance waits for user confirmation.
func FormatWaitingAlert(instID, title, workspace string, waitSec int64) string {
	if strings.TrimSpace(title) == "" {
		title = "未命名会话"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🟡 <b>AgyMate · 等待确认</b>\n")
	fmt.Fprintf(&b, "实例: <code>%s</code>\n", html.EscapeString(strings.ToUpper(instID)))
	fmt.Fprintf(&b, "会话: %s\n", html.EscapeString(title))
	if workspace != "" {
		fmt.Fprintf(&b, "工作区: <code>%s</code>\n", html.EscapeString(workspace))
	}
	fmt.Fprintf(&b, "已等待: %s（可能停留在命令审批或提问选项）", formatDurationCN(waitSec))
	return b.String()
}

// FormatLowQuotaAlert renders a Telegram alert when a quota bucket drops below threshold.
func FormatLowQuotaAlert(instID, groupName, window string, remainingPct float64, resetTime string) string {
	winCN := localizeWindowName(window)
	poolLabel := strings.TrimSpace(groupName)
	if winCN != "" && !strings.Contains(poolLabel, winCN) {
		poolLabel = fmt.Sprintf("%s（%s）", poolLabel, winCN)
	}
	return fmt.Sprintf(
		"⚠️ <b>AgyMate · 配额低水位预警</b>\n账号/实例: <code>%s</code>\n配额项: %s\n剩余额度: <b>%.1f%%</b>\n重置时间: <code>%s</code>",
		html.EscapeString(instID),
		html.EscapeString(poolLabel),
		remainingPct,
		html.EscapeString(formatResetTimeCN(resetTime)),
	)
}

// FormatColabBurnAlert renders a Telegram alert when Google Colab VMs remain active or orphaned for too long.
func FormatColabBurnAlert(account string, assignments, orphans, activeMins int, rateHourly, balance float64, hwSummary string) string {
	heading := "Colab 云端实例持续运行提醒"
	icon := "🔥"
	if orphans > 0 {
		icon = "🚨"
		heading = "检测到 Colab 孤儿实例持续扣费"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s <b>AgyMate · %s</b>\n", icon, heading)
	if account != "" {
		fmt.Fprintf(&b, "账号: <code>%s</code>\n", html.EscapeString(account))
	}
	fmt.Fprintf(&b, "活跃实例: <b>%d 台</b>（孤儿实例: <b>%d 台</b>）\n", assignments, orphans)
	if hwSummary != "" {
		fmt.Fprintf(&b, "加速规格: <code>%s</code>\n", html.EscapeString(hwSummary))
	}
	fmt.Fprintf(&b, "持续时长: <b>%d 分钟</b>\n", activeMins)
	fmt.Fprintf(&b, "消耗速率: <b>%.2f CCU/小时</b> · 余额: <b>%.2f CCU</b>\n", rateHourly, balance)
	fmt.Fprintf(&b, "可在 AgyMate 控制台点击「一键清场」或执行 <code>agymate colab nuke</code>。")
	return b.String()
}

// FormatColabLowBalanceAlert renders a Telegram alert when Colab CCU drops below threshold.
func FormatColabLowBalanceAlert(account string, balance, rateHourly float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "⚠️ <b>AgyMate · Colab 算力低水位预警</b>\n")
	if account != "" {
		fmt.Fprintf(&b, "账号: <code>%s</code>\n", html.EscapeString(account))
	}
	fmt.Fprintf(&b, "剩余算力: <b>%.2f CCU</b>\n", balance)
	fmt.Fprintf(&b, "当前速率: <code>%.2f CCU/小时</code>", rateHourly)
	return b.String()
}
