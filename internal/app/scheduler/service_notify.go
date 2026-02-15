package scheduler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

func (s *Service) sendPersonaNotification(ctx context.Context, task *domain.ScheduledTask, prompt string) error {
	notifier, generator, timeout := s.deliverySnapshot()
	if notifier == nil {
		return fmt.Errorf("notifier is not configured")
	}
	if generator == nil {
		return fmt.Errorf("message generator is not configured")
	}

	notifyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	content, err := generator.GeneratePersonaNotification(notifyCtx, task.PersonaID, task.ChannelID, prompt)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(content)
	if text == "" {
		return fmt.Errorf("generated notification is empty")
	}
	message := fmt.Sprintf("<@%s> %s", task.TargetUserID, text)
	return notifier.SendMessage(task.ChannelID, message)
}

func (s *Service) sendFallbackNotification(_ context.Context, task *domain.ScheduledTask, fallback string) error {
	notifier := s.notifierSnapshot()
	if notifier == nil {
		return fmt.Errorf("notifier is not configured")
	}
	text := strings.TrimSpace(fallback)
	if text == "" {
		text = "任務已觸發。"
	}
	return notifier.SendMessage(task.ChannelID, fmt.Sprintf("<@%s> %s", task.TargetUserID, text))
}

func buildReminderPrompt(task *domain.ScheduledTask, now time.Time) string {
	reminder := safeReminderText(task.ReminderText)
	return fmt.Sprintf(
		"你要在 Discord 對使用者送出提醒。現在時間：%s。提醒內容：%s。請用角色語氣寫一則簡短提醒訊息。",
		now.Format(time.RFC3339),
		reminder,
	)
}

func buildMonitorViolationPrompt(task *domain.ScheduledTask, now time.Time) string {
	count := 1
	if task.PendingAlert != nil && task.PendingAlert.ViolationCount > 0 {
		count = task.PendingAlert.ViolationCount
	}
	return fmt.Sprintf(
		"你在 Discord 背景監控中偵測到目標使用者在限制期間有活動。現在時間：%s。偵測到 %d 則新活動。請用角色語氣發出簡短警告。",
		now.Format(time.RFC3339),
		count,
	)
}

func safeReminderText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "提醒時間到了。"
	}
	return text
}

func truncateSchedulerError(text string, maxLen int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if maxLen <= 0 || len(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return text[:maxLen]
	}
	return text[:maxLen-3] + "..."
}
