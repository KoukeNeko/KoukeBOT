package util

import (
	"fmt"
	"time"
)

// FormatUserTime 格式化為 OpenClaw 風格的友善時間字串
// 範例：Wednesday, February 9th, 2026 — 13:45 (Asia/Taipei)
func FormatUserTime(t time.Time, timezone string, format24h bool) string {
	// 載入時區
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		// 如果時區無效，使用本地時區
		loc = time.Local
	}
	localTime := t.In(loc)

	// 星期幾，月份，日期（帶序數詞），年份
	weekday := localTime.Format("Monday")
	month := localTime.Format("January")
	day := localTime.Day()
	daySuffix := getDaySuffix(day)
	year := localTime.Year()

	// 時間格式
	var timeStr string
	if format24h {
		timeStr = localTime.Format("15:04")
	} else {
		timeStr = localTime.Format("3:04 PM")
	}

	return fmt.Sprintf("%s, %s %d%s, %d — %s (%s)",
		weekday, month, day, daySuffix, year, timeStr, timezone)
}

// FormatTimezoneOnly 僅返回時區字串（用於 system prompt 注入）
// 這樣可以保持 system prompt 靜態，避免破壞 prompt cache
func FormatTimezoneOnly(timezone string) string {
	if timezone == "" || timezone == "auto" {
		// 使用系統本地時區
		_, offset := time.Now().Zone()
		hours := offset / 3600
		return fmt.Sprintf("Time zone: UTC%+d (Local)", hours)
	}
	return fmt.Sprintf("Time zone: %s", timezone)
}

// getDaySuffix 返回英文序數詞後綴（1st, 2nd, 3rd, 4th...）
func getDaySuffix(day int) string {
	// 特殊情況：11, 12, 13 都是 "th"
	if day >= 11 && day <= 13 {
		return "th"
	}
	// 根據個位數決定
	switch day % 10 {
	case 1:
		return "st"
	case 2:
		return "nd"
	case 3:
		return "rd"
	default:
		return "th"
	}
}
