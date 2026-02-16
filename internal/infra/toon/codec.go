package tooncodec

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	toon "github.com/toon-format/toon-go"
)

const (
	EventTypeMessage     = "msg"
	EventTypeContext     = "ctx"
	EventTypeToolResults = "toolr"

	PriorityNormal    = "normal"
	PriorityReplyHigh = "reply_high"
)

type MessageEventInput struct {
	Source    string
	UserID    string
	Name      string
	ChannelID string
	PersonaID string
	Timestamp time.Time
	Text      string
	Priority  string
}

type ContextEventInput struct {
	Source    string
	ChannelID string
	PersonaID string
	History   []string
	Current   string
}

type ToolResultInput struct {
	ID   string
	Name string
	OK   bool
	Out  string
}

type messageEvent struct {
	Version  int    `toon:"v"`
	Type     string `toon:"t"`
	Source   string `toon:"src"`
	UserID   string `toon:"uid,omitempty"`
	Name     string `toon:"name,omitempty"`
	Channel  string `toon:"cid,omitempty"`
	Persona  string `toon:"pid,omitempty"`
	TS       int64  `toon:"ts"`
	Text     string `toon:"txt"`
	Priority string `toon:"pri,omitempty"`
}

type contextEvent struct {
	Version int            `toon:"v"`
	Type    string         `toon:"t"`
	Source  string         `toon:"src"`
	Channel string         `toon:"cid,omitempty"`
	Persona string         `toon:"pid,omitempty"`
	History []messageEvent `toon:"hist,omitempty"`
	Current messageEvent   `toon:"cur"`
}

type toolResultEventItem struct {
	ID   string `toon:"id,omitempty"`
	Name string `toon:"name,omitempty"`
	OK   bool   `toon:"ok"`
	Out  string `toon:"out,omitempty"`
}

type toolResultsEvent struct {
	Version int                   `toon:"v"`
	Type    string                `toon:"t"`
	Source  string                `toon:"src"`
	Items   []toolResultEventItem `toon:"items,omitempty"`
}

func EncodeMessageEvent(input MessageEventInput) (string, error) {
	ts := input.Timestamp.UTC()
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	priority := strings.TrimSpace(input.Priority)
	if priority == "" {
		priority = PriorityNormal
	}

	event := messageEvent{
		Version:  1,
		Type:     EventTypeMessage,
		Source:   normalizeSource(input.Source),
		UserID:   strings.TrimSpace(input.UserID),
		Name:     strings.TrimSpace(input.Name),
		Channel:  strings.TrimSpace(input.ChannelID),
		Persona:  strings.TrimSpace(input.PersonaID),
		TS:       ts.Unix(),
		Text:     input.Text,
		Priority: priority,
	}
	return marshalStrict(event)
}

func EncodeContextEvent(input ContextEventInput) (string, error) {
	current, err := decodeMessageEvent(input.Current)
	if err != nil {
		return "", fmt.Errorf("decode current message event: %w", err)
	}

	history := make([]messageEvent, 0, len(input.History))
	for _, raw := range input.History {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		msg, err := decodeMessageEvent(raw)
		if err != nil {
			return "", fmt.Errorf("decode history message event: %w", err)
		}
		history = append(history, msg)
	}

	channelID := strings.TrimSpace(input.ChannelID)
	if channelID == "" {
		channelID = current.Channel
	}
	personaID := strings.TrimSpace(input.PersonaID)
	if personaID == "" {
		personaID = current.Persona
	}

	event := contextEvent{
		Version: 1,
		Type:    EventTypeContext,
		Source:  normalizeSource(input.Source),
		Channel: channelID,
		Persona: personaID,
		History: history,
		Current: current,
	}
	return marshalStrict(event)
}

func EncodeToolResultsEvent(items []ToolResultInput) (string, error) {
	encodedItems := make([]toolResultEventItem, 0, len(items))
	for _, item := range items {
		encodedItems = append(encodedItems, toolResultEventItem{
			ID:   strings.TrimSpace(item.ID),
			Name: strings.TrimSpace(item.Name),
			OK:   item.OK,
			Out:  strings.TrimSpace(item.Out),
		})
	}
	event := toolResultsEvent{
		Version: 1,
		Type:    EventTypeToolResults,
		Source:  "system",
		Items:   encodedItems,
	}
	return marshalStrict(event)
}

func DecodeEventType(text string) (string, bool) {
	obj, ok := decodeToMap(text)
	if !ok {
		return "", false
	}
	t := strings.TrimSpace(asString(obj["t"]))
	if t == "" {
		return "", false
	}
	return t, true
}

func ExtractMessageText(text string) (string, bool) {
	msg, err := decodeMessageEvent(strings.TrimSpace(text))
	if err != nil {
		return "", false
	}
	return msg.Text, true
}

func HumanizeTOON(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}

	obj, ok := decodeToMap(trimmed)
	if !ok {
		return text
	}
	t := strings.TrimSpace(asString(obj["t"]))
	switch t {
	case EventTypeMessage:
		msg, err := decodeMessageEvent(trimmed)
		if err != nil {
			return text
		}
		return humanizeMessage(msg)
	case EventTypeContext:
		return humanizeContext(obj)
	case EventTypeToolResults:
		return humanizeToolResults(obj)
	default:
		return text
	}
}

func normalizeSource(source string) string {
	s := strings.ToLower(strings.TrimSpace(source))
	switch s {
	case "discord", "web", "api":
		return s
	default:
		return "api"
	}
}

func marshalStrict(v any) (string, error) {
	return toon.MarshalString(v, toon.WithLengthMarkers(true))
}

func decodeToMap(text string) (map[string]any, bool) {
	decoded, err := toon.DecodeString(strings.TrimSpace(text))
	if err != nil {
		return nil, false
	}
	obj, ok := decoded.(map[string]any)
	if !ok {
		return nil, false
	}
	return obj, true
}

func decodeMessageEvent(text string) (messageEvent, error) {
	obj, ok := decodeToMap(text)
	if !ok {
		return messageEvent{}, fmt.Errorf("not a TOON object")
	}
	typeName := strings.TrimSpace(asString(obj["t"]))
	if typeName != EventTypeMessage {
		return messageEvent{}, fmt.Errorf("event type %q is not %q", typeName, EventTypeMessage)
	}

	msg := messageEvent{
		Version:  asInt(obj["v"], 1),
		Type:     EventTypeMessage,
		Source:   normalizeSource(asString(obj["src"])),
		UserID:   strings.TrimSpace(asString(obj["uid"])),
		Name:     strings.TrimSpace(asString(obj["name"])),
		Channel:  strings.TrimSpace(asString(obj["cid"])),
		Persona:  strings.TrimSpace(asString(obj["pid"])),
		TS:       asInt64(obj["ts"], time.Now().UTC().Unix()),
		Text:     asString(obj["txt"]),
		Priority: strings.TrimSpace(asString(obj["pri"])),
	}
	if msg.Priority == "" {
		msg.Priority = PriorityNormal
	}
	return msg, nil
}

func asString(v any) string {
	switch raw := v.(type) {
	case string:
		return raw
	case fmt.Stringer:
		return raw.String()
	case float64:
		return strconv.FormatFloat(raw, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(raw), 'f', -1, 64)
	case int:
		return strconv.Itoa(raw)
	case int32:
		return strconv.FormatInt(int64(raw), 10)
	case int64:
		return strconv.FormatInt(raw, 10)
	case uint:
		return strconv.FormatUint(uint64(raw), 10)
	case uint32:
		return strconv.FormatUint(uint64(raw), 10)
	case uint64:
		return strconv.FormatUint(raw, 10)
	case bool:
		return strconv.FormatBool(raw)
	default:
		return ""
	}
}

func asInt(v any, fallback int) int {
	s := asString(v)
	if s == "" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}

func asInt64(v any, fallback int64) int64 {
	s := asString(v)
	if s == "" {
		return fallback
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		f, ferr := strconv.ParseFloat(s, 64)
		if ferr != nil {
			return fallback
		}
		return int64(f)
	}
	return n
}

func humanizeMessage(msg messageEvent) string {
	var lines []string
	header := fmt.Sprintf("[TOON msg] src=%s uid=%s name=%s cid=%s pid=%s ts=%d pri=%s",
		nonEmpty(msg.Source, "-"),
		nonEmpty(msg.UserID, "-"),
		nonEmpty(msg.Name, "-"),
		nonEmpty(msg.Channel, "-"),
		nonEmpty(msg.Persona, "-"),
		msg.TS,
		nonEmpty(msg.Priority, PriorityNormal),
	)
	lines = append(lines, header)
	if msg.Text != "" {
		lines = append(lines, msg.Text)
	}
	return strings.Join(lines, "\n")
}

func humanizeContext(obj map[string]any) string {
	var sb strings.Builder
	sb.WriteString("[TOON ctx]")
	sb.WriteString(fmt.Sprintf(" src=%s cid=%s pid=%s\n",
		nonEmpty(asString(obj["src"]), "-"),
		nonEmpty(asString(obj["cid"]), "-"),
		nonEmpty(asString(obj["pid"]), "-"),
	))

	if hist, ok := obj["hist"].([]any); ok && len(hist) > 0 {
		sb.WriteString("History:\n")
		for idx, raw := range hist {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			sb.WriteString(fmt.Sprintf("%d. uid=%s name=%s pri=%s txt=%s\n",
				idx+1,
				nonEmpty(asString(entry["uid"]), "-"),
				nonEmpty(asString(entry["name"]), "-"),
				nonEmpty(asString(entry["pri"]), PriorityNormal),
				truncate(asString(entry["txt"]), 180),
			))
		}
	}

	if cur, ok := obj["cur"].(map[string]any); ok {
		sb.WriteString("Current:\n")
		sb.WriteString(fmt.Sprintf("uid=%s name=%s pri=%s txt=%s",
			nonEmpty(asString(cur["uid"]), "-"),
			nonEmpty(asString(cur["name"]), "-"),
			nonEmpty(asString(cur["pri"]), PriorityNormal),
			asString(cur["txt"]),
		))
	}

	return strings.TrimSpace(sb.String())
}

func humanizeToolResults(obj map[string]any) string {
	var sb strings.Builder
	sb.WriteString("[TOON tool results]\n")
	if items, ok := obj["items"].([]any); ok && len(items) > 0 {
		for i, raw := range items {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			sb.WriteString(fmt.Sprintf("%d. %s (%s) status=%s\n",
				i+1,
				nonEmpty(asString(entry["name"]), "unknown"),
				nonEmpty(asString(entry["id"]), "n/a"),
				boolLabel(entry["ok"]),
			))
			out := strings.TrimSpace(asString(entry["out"]))
			if out != "" {
				sb.WriteString("   ")
				sb.WriteString(truncate(out, 240))
				sb.WriteString("\n")
			}
		}
	}
	return strings.TrimSpace(sb.String())
}

func boolLabel(v any) string {
	switch raw := v.(type) {
	case bool:
		if raw {
			return "ok"
		}
		return "error"
	case string:
		if raw == "true" {
			return "ok"
		}
	}
	return "error"
}

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
