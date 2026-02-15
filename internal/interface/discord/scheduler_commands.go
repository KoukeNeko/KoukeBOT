package discord

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	schedulerpkg "github.com/KoukeNeko/KoukeBOT/internal/app/scheduler"
	"github.com/bwmarrin/discordgo"
)

var (
	strictReminderPattern = regexp.MustCompile(`(?i)(\d+)\s*(秒|分鐘|分|小時|時|天)\s*後\s*(.+)$`)
	strictMonitorWindow   = regexp.MustCompile(`(?i)(?:接下來|未來)?\s*(\d+)\s*(秒|分鐘|分|小時|時|天)\s*內`)
)

func (h *Handler) tryHandleSchedulerStrict(ctx context.Context, m *discordgo.MessageCreate, personaID, content string) bool {
	if h == nil || h.schedulerService == nil || m == nil {
		return false
	}

	normalized := normalizeSchedulerText(content)
	if normalized == "" {
		return false
	}

	reminderDelay, reminderText, ok := parseStrictReminder(normalized)
	if ok {
		if err := h.handleStrictReminder(ctx, m, personaID, reminderDelay, reminderText); err != nil {
			logHandler.Warnf("strict reminder failed [%s]: %v", m.ChannelID, err)
			_ = h.bot.SendMessage(m.ChannelID, "❌ 排程提醒建立失敗："+err.Error())
		}
		return true
	}

	monitorDuration, ok := parseStrictMonitor(normalized)
	if ok {
		if err := h.handleStrictMonitor(ctx, m, personaID, monitorDuration); err != nil {
			logHandler.Warnf("strict monitor failed [%s]: %v", m.ChannelID, err)
			_ = h.bot.SendMessage(m.ChannelID, "❌ 背景監控建立失敗："+err.Error())
		}
		return true
	}

	return false
}

func (h *Handler) handleStrictReminder(ctx context.Context, m *discordgo.MessageCreate, personaID string, delay time.Duration, reminderText string) error {
	now := time.Now().UTC()
	runAt := now.Add(delay)
	task, err := h.schedulerService.CreateReminder(ctx, schedulerpkg.CreateReminderRequest{
		PersonaID:     personaID,
		ChannelID:     m.ChannelID,
		CreatorUserID: m.Author.ID,
		TargetUserID:  m.Author.ID,
		ReminderText:  reminderText,
		RunAt:         runAt,
	})
	if err != nil {
		return err
	}

	confirm := fmt.Sprintf(
		"✅ 已設定提醒（`%s`），將在 <t:%d:R> 提醒你。",
		task.ID,
		runAt.Unix(),
	)
	if strings.TrimSpace(reminderText) != "" {
		confirm += "\n📝 內容：" + reminderText
	}
	return h.bot.SendMessage(m.ChannelID, confirm)
}

func (h *Handler) handleStrictMonitor(ctx context.Context, m *discordgo.MessageCreate, personaID string, duration time.Duration) error {
	targets := strictMonitorTargets(m, h.bot.Session())
	created := make([]string, 0, len(targets))
	failures := make([]string, 0)
	for _, targetID := range targets {
		task, err := h.schedulerService.CreateMonitor(ctx, schedulerpkg.CreateMonitorRequest{
			PersonaID:     personaID,
			ChannelID:     m.ChannelID,
			CreatorUserID: m.Author.ID,
			TargetUserID:  targetID,
			Duration:      duration,
		})
		if err != nil {
			failures = append(failures, fmt.Sprintf("<@%s>: %s", targetID, err.Error()))
			continue
		}
		created = append(created, task.ID)
	}
	if len(created) == 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}

	confirm := fmt.Sprintf(
		"✅ 已啟用背景監控，時長 %s，目標 %d 人。",
		formatDurationForDisplay(duration),
		len(targets),
	)
	confirm += "\n任務 ID：" + strings.Join(created, ", ")
	if len(failures) > 0 {
		confirm += "\n⚠️ 部分目標失敗：" + strings.Join(failures, "; ")
	}
	return h.bot.SendMessage(m.ChannelID, confirm)
}

func parseStrictReminder(content string) (time.Duration, string, bool) {
	matches := strictReminderPattern.FindStringSubmatch(content)
	if len(matches) != 4 {
		return 0, "", false
	}
	delay, ok := parseChineseDuration(matches[1], matches[2])
	if !ok || delay <= 0 {
		return 0, "", false
	}
	tail := strings.TrimSpace(matches[3])
	if tail == "" {
		return 0, "", false
	}
	if !containsAny(tail, []string{"提醒", "叫我", "通知"}) {
		return 0, "", false
	}
	tail = strings.TrimPrefix(tail, "叫我")
	tail = strings.TrimPrefix(tail, "提醒我")
	tail = strings.TrimPrefix(tail, "通知我")
	tail = strings.TrimSpace(tail)
	if tail == "" {
		tail = "提醒時間到了。"
	}
	return delay, tail, true
}

func parseStrictMonitor(content string) (time.Duration, bool) {
	if !containsAny(content, []string{"活動", "訊息", "紀錄", "發言"}) {
		return 0, false
	}
	if !containsAny(content, []string{"別讓我看到", "不要讓我看到", "監控", "監看", "觀察"}) {
		return 0, false
	}
	matches := strictMonitorWindow.FindStringSubmatch(content)
	if len(matches) != 3 {
		return 0, false
	}
	duration, ok := parseChineseDuration(matches[1], matches[2])
	if !ok || duration <= 0 {
		return 0, false
	}
	return duration, true
}

func strictMonitorTargets(m *discordgo.MessageCreate, session *discordgo.Session) []string {
	requester := ""
	if m != nil && m.Author != nil {
		requester = m.Author.ID
	}
	botID := ""
	if session != nil && session.State != nil && session.State.User != nil {
		botID = session.State.User.ID
	}

	targets := make([]string, 0, len(m.Mentions)+1)
	if requester != "" {
		targets = append(targets, requester)
	}
	for _, user := range m.Mentions {
		if user == nil || strings.TrimSpace(user.ID) == "" {
			continue
		}
		if user.ID == botID {
			continue
		}
		targets = append(targets, user.ID)
	}

	seen := make(map[string]struct{}, len(targets))
	unique := make([]string, 0, len(targets))
	for _, id := range targets {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}

func parseChineseDuration(numberText, unit string) (time.Duration, bool) {
	numberText = strings.TrimSpace(numberText)
	unit = strings.TrimSpace(unit)
	if numberText == "" || unit == "" {
		return 0, false
	}
	value := int64(0)
	for _, ch := range numberText {
		if ch < '0' || ch > '9' {
			return 0, false
		}
		value = value*10 + int64(ch-'0')
	}
	if value <= 0 {
		return 0, false
	}

	switch unit {
	case "秒":
		return time.Duration(value) * time.Second, true
	case "分", "分鐘":
		return time.Duration(value) * time.Minute, true
	case "時", "小時":
		return time.Duration(value) * time.Hour, true
	case "天":
		return time.Duration(value) * 24 * time.Hour, true
	default:
		return 0, false
	}
}

func normalizeSchedulerText(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"　", " ",
		"，", " ",
		"。", " ",
		"\n", " ",
		"\r", " ",
	)
	content = replacer.Replace(content)
	fields := strings.Fields(content)
	return strings.TrimSpace(strings.Join(fields, " "))
}

func containsAny(text string, keywords []string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func formatDurationForDisplay(d time.Duration) string {
	if d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%d 天", int(d/(24*time.Hour)))
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%d 小時", int(d/time.Hour))
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%d 分鐘", int(d/time.Minute))
	}
	return d.String()
}
