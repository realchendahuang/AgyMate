package notify

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFormatNotificationsChineseAndUTF8SafeTruncation(t *testing.T) {
	longErr := strings.Repeat("模型返回异常中文提示信息", 30)
	stopMsg := FormatSessionStop("main", "构建并测试后端", "AgyMate", "Gemini 4 Carbon", "EXCEEDED_MAX_STEPS", longErr, 125, true)
	if !utf8.ValidString(stopMsg) {
		t.Fatal("FormatSessionStop produced invalid UTF-8 after truncation")
	}
	for _, want := range []string{"任务异常中断", "2 分 05 秒", "达到最大执行步数上限"} {
		if !strings.Contains(stopMsg, want) {
			t.Fatalf("expected stopMsg to contain %q, got:\n%s", want, stopMsg)
		}
	}

	waitMsg := FormatWaitingAlert("p1", "", "demo", 75)
	if !strings.Contains(waitMsg, "未命名会话") || !strings.Contains(waitMsg, "1 分 15 秒") {
		t.Fatalf("unexpected waitMsg:\n%s", waitMsg)
	}

	quotaMsg := FormatLowQuotaAlert("dev@example.com", "Gemini · 5小时限额", "5h", 9.5, "2030-01-01T00:00:00Z")
	if !strings.Contains(quotaMsg, "配额低水位预警") || !strings.Contains(quotaMsg, "Gemini · 5小时限额") {
		t.Fatalf("unexpected quotaMsg:\n%s", quotaMsg)
	}
}
