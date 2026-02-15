package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	discordruntime "github.com/KoukeNeko/KoukeBOT/internal/infra/discord"
	"github.com/bwmarrin/discordgo"
)

const (
	discordObserveActionCapabilities = "capabilities"
	discordObserveActionGuildInfo    = "guild_info"
	discordObserveActionListChannels = "list_channels"
	discordObserveActionListRoles    = "list_roles"
	discordObserveActionListMembers  = "list_members"
	discordObserveActionGetMember    = "get_member"
	discordObserveActionListPresence = "list_presences"
	discordObserveActionListMessages = "list_messages"
)

var (
	discordToolCallsTotal             uint64
	discordToolDeniedNonDiscordSource uint64
	discordToolDeniedCrossGuild       uint64
	discordToolDeniedDM               uint64
	discordToolDegradedMissingIntents uint64
)

// DiscordObserveTool provides read-only Discord observation capabilities.
// This tool is runtime-bound and only available for Discord-origin requests.
type DiscordObserveTool struct {
	runtime *discordruntime.Runtime
}

func NewDiscordObserveTool(runtime *discordruntime.Runtime) *DiscordObserveTool {
	return &DiscordObserveTool{runtime: runtime}
}

func (t *DiscordObserveTool) Name() string {
	return "discord_observe"
}

func (t *DiscordObserveTool) Description() string {
	return "Read-only Discord observer for the current guild context. Available only for Discord-origin requests. Supports capabilities, guild info, channels, roles, members, presences, and messages."
}

func (t *DiscordObserveTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type": "string",
				"enum": []string{
					discordObserveActionCapabilities,
					discordObserveActionGuildInfo,
					discordObserveActionListChannels,
					discordObserveActionListRoles,
					discordObserveActionListMembers,
					discordObserveActionGetMember,
					discordObserveActionListPresence,
					discordObserveActionListMessages,
				},
				"description": "Discord observe action to execute.",
			},
			"channel_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional channel ID. list_messages can target any channel in current guild when message scope allows it.",
			},
			"user_id": map[string]interface{}{
				"type":        "string",
				"description": "Target user ID for get_member.",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Result limit (default from settings, max 100).",
				"minimum":     1,
				"maximum":     100,
			},
			"before": map[string]interface{}{
				"type":        "string",
				"description": "Optional message ID cursor for list_messages pagination.",
			},
		},
		"required": []string{"action"},
	}
}

func (t *DiscordObserveTool) Execute(ctx context.Context, input map[string]interface{}) (domain.ToolResult, error) {
	metricLogf("discord_tool.calls_total", &discordToolCallsTotal)

	action := readString(input, "action")
	if action == "" {
		logTool.Warnf("discord_observe rejected: missing action field")
		return t.errorResult(action, "missing required field: action", nil, map[string]any{})
	}

	reqCtx, ok := GetRequestContext(ctx)
	if !ok || reqCtx.Source != RequestSourceDiscord {
		metricLogf("discord_tool.denied_non_discord_source", &discordToolDeniedNonDiscordSource)
		logTool.Warnf("discord_observe denied: action=%s reason=non_discord_source source=%s", action, reqCtx.Source)
		return t.errorResult(action, "discord_observe is only available for Discord-origin requests", nil, map[string]any{
			"source": reqCtx.Source,
		})
	}
	if strings.TrimSpace(reqCtx.GuildID) == "" {
		metricLogf("discord_tool.denied_dm", &discordToolDeniedDM)
		logTool.Warnf("discord_observe denied: action=%s reason=dm_context channel=%s", action, reqCtx.ChannelID)
		return t.errorResult(action, "discord_observe is disabled in DM context", nil, map[string]any{
			"source":   reqCtx.Source,
			"guild_id": reqCtx.GuildID,
		})
	}
	if t.runtime == nil {
		logTool.Warnf("discord_observe denied: action=%s reason=runtime_not_configured guild=%s", action, reqCtx.GuildID)
		return t.errorResult(action, "discord runtime not configured", nil, map[string]any{
			"guild_id": reqCtx.GuildID,
		})
	}

	limit := t.parseLimit(input)
	before := readString(input, "before")
	channelID := readString(input, "channel_id")
	userID := readString(input, "user_id")

	logTool.Logf(
		"discord_observe start: action=%s guild=%s channel=%s requested_channel=%s user=%s limit=%d before=%s",
		action,
		summarizeDiscordID(reqCtx.GuildID),
		summarizeDiscordID(reqCtx.ChannelID),
		summarizeDiscordID(channelID),
		summarizeDiscordID(userID),
		limit,
		truncateObserveLogField(before, 40),
	)

	var (
		result domain.ToolResult
		err    error
	)

	switch action {
	case discordObserveActionCapabilities:
		result = t.capabilitiesResult(reqCtx)
	case discordObserveActionGuildInfo:
		result, err = t.guildInfo(ctx, reqCtx, action)
	case discordObserveActionListChannels:
		result, err = t.listChannels(ctx, reqCtx, action)
	case discordObserveActionListRoles:
		result, err = t.listRoles(ctx, reqCtx, action)
	case discordObserveActionListMembers:
		result, err = t.listMembers(ctx, reqCtx, action, limit)
	case discordObserveActionGetMember:
		if userID == "" {
			result, err = t.errorResult(action, "user_id is required for get_member", nil, t.baseMeta(reqCtx, action))
			break
		}
		result, err = t.getMember(ctx, reqCtx, action, userID)
	case discordObserveActionListPresence:
		result, err = t.listPresences(reqCtx, action)
	case discordObserveActionListMessages:
		result, err = t.listMessages(ctx, reqCtx, action, channelID, before, limit)
	default:
		result, err = t.errorResult(action, "unsupported action", nil, t.baseMeta(reqCtx, action))
	}

	logObserveCompletion(action, result, err)
	return result, err
}

func (t *DiscordObserveTool) capabilitiesResult(reqCtx RequestContext) domain.ToolResult {
	caps := t.runtime.Capabilities()
	warnings := make([]string, 0, 2)
	if !caps.MemberIntentEnabled {
		warnings = append(warnings, "discord_member_intent_enabled=false; list_members may be degraded or unavailable.")
		metricLogf("discord_tool.degraded_missing_intent", &discordToolDegradedMissingIntents)
	}
	if !caps.PresenceIntentEnabled {
		warnings = append(warnings, "discord_presence_intent_enabled=false; list_presences returns empty with warning.")
		metricLogf("discord_tool.degraded_missing_intent", &discordToolDegradedMissingIntents)
	}

	data := map[string]any{
		"ready":                   caps.Ready,
		"member_intent_enabled":   caps.MemberIntentEnabled,
		"presence_intent_enabled": caps.PresenceIntentEnabled,
		"message_scope":           caps.MessageScope,
		"max_results":             caps.MaxResults,
		"available_actions": []string{
			discordObserveActionCapabilities,
			discordObserveActionGuildInfo,
			discordObserveActionListChannels,
			discordObserveActionListRoles,
			discordObserveActionListMembers,
			discordObserveActionGetMember,
			discordObserveActionListPresence,
			discordObserveActionListMessages,
		},
	}

	meta := map[string]any{
		"action":     discordObserveActionCapabilities,
		"source":     reqCtx.Source,
		"guild_id":   reqCtx.GuildID,
		"channel_id": reqCtx.ChannelID,
	}
	return encodeObserveResult(data, meta, warnings, "", false)
}

func (t *DiscordObserveTool) guildInfo(ctx context.Context, reqCtx RequestContext, action string) (domain.ToolResult, error) {
	guild, err := t.runtime.Guild(ctx, reqCtx.GuildID)
	if err != nil {
		return t.errorResult(action, "fetch guild info failed: "+err.Error(), nil, t.baseMeta(reqCtx, action))
	}

	data := map[string]any{
		"id":               guild.ID,
		"name":             guild.Name,
		"description":      guild.Description,
		"owner_id":         guild.OwnerID,
		"member_count":     guild.MemberCount,
		"premium_tier":     guild.PremiumTier,
		"preferred_locale": guild.PreferredLocale,
		"nsfw_level":       guild.NSFWLevel,
		"icon":             guild.Icon,
	}

	return encodeObserveResult(data, t.baseMeta(reqCtx, action), nil, "", false), nil
}

func (t *DiscordObserveTool) listChannels(ctx context.Context, reqCtx RequestContext, action string) (domain.ToolResult, error) {
	channels, err := t.runtime.GuildChannels(ctx, reqCtx.GuildID)
	if err != nil {
		return t.errorResult(action, "list channels failed: "+err.Error(), nil, t.baseMeta(reqCtx, action))
	}

	sort.Slice(channels, func(i, j int) bool {
		if channels[i].Position == channels[j].Position {
			return channels[i].Name < channels[j].Name
		}
		return channels[i].Position < channels[j].Position
	})

	items := make([]map[string]any, 0, len(channels))
	for _, ch := range channels {
		if ch == nil {
			continue
		}
		items = append(items, map[string]any{
			"id":          ch.ID,
			"name":        ch.Name,
			"type":        channelTypeName(ch.Type),
			"position":    ch.Position,
			"parent_id":   ch.ParentID,
			"topic":       ch.Topic,
			"nsfw":        ch.NSFW,
			"is_archived": ch.ThreadMetadata != nil && ch.ThreadMetadata.Archived,
		})
	}

	meta := t.baseMeta(reqCtx, action)
	meta["count"] = len(items)
	return encodeObserveResult(items, meta, nil, "", false), nil
}

func (t *DiscordObserveTool) listRoles(ctx context.Context, reqCtx RequestContext, action string) (domain.ToolResult, error) {
	guild, err := t.runtime.Guild(ctx, reqCtx.GuildID)
	if err != nil {
		return t.errorResult(action, "fetch guild roles failed: "+err.Error(), nil, t.baseMeta(reqCtx, action))
	}

	roles := append([]*discordgo.Role(nil), guild.Roles...)
	sort.Slice(roles, func(i, j int) bool {
		if roles[i].Position == roles[j].Position {
			return roles[i].ID < roles[j].ID
		}
		return roles[i].Position > roles[j].Position
	})

	items := make([]map[string]any, 0, len(roles))
	for _, role := range roles {
		if role == nil {
			continue
		}
		items = append(items, map[string]any{
			"id":          role.ID,
			"name":        role.Name,
			"position":    role.Position,
			"color":       role.Color,
			"hoist":       role.Hoist,
			"mentionable": role.Mentionable,
			"managed":     role.Managed,
		})
	}

	meta := t.baseMeta(reqCtx, action)
	meta["count"] = len(items)
	return encodeObserveResult(items, meta, nil, "", false), nil
}

func (t *DiscordObserveTool) listMembers(ctx context.Context, reqCtx RequestContext, action string, limit int) (domain.ToolResult, error) {
	members, err := t.runtime.GuildMembers(ctx, reqCtx.GuildID, "", limit)
	if err != nil {
		if errors.Is(err, discordruntime.ErrMissingMemberIntent) {
			metricLogf("discord_tool.degraded_missing_intent", &discordToolDegradedMissingIntents)
			meta := t.baseMeta(reqCtx, action)
			meta["limit"] = limit
			meta["count"] = 0
			meta["has_more"] = false
			warnings := []string{
				"Missing privileged intent: enable discord_member_intent_enabled and restart bot to use list_members.",
			}
			return encodeObserveResult([]map[string]any{}, meta, warnings, "", false), nil
		}
		return t.errorResult(action, "list members failed: "+err.Error(), nil, t.baseMeta(reqCtx, action))
	}

	items := make([]map[string]any, 0, len(members))
	for _, member := range members {
		if member == nil || member.User == nil {
			continue
		}
		display := member.User.GlobalName
		if strings.TrimSpace(display) == "" {
			display = member.User.Username
		}
		items = append(items, map[string]any{
			"user_id":      member.User.ID,
			"username":     member.User.Username,
			"display_name": display,
			"nick":         member.Nick,
			"bot":          member.User.Bot,
			"roles":        member.Roles,
			"joined_at":    member.JoinedAt,
		})
	}

	meta := t.baseMeta(reqCtx, action)
	meta["limit"] = limit
	meta["count"] = len(items)
	meta["has_more"] = len(items) >= limit
	return encodeObserveResult(items, meta, nil, "", false), nil
}

func (t *DiscordObserveTool) getMember(ctx context.Context, reqCtx RequestContext, action, userID string) (domain.ToolResult, error) {
	member, err := t.runtime.GuildMember(ctx, reqCtx.GuildID, userID)
	if err != nil {
		return t.errorResult(action, "get member failed: "+err.Error(), nil, t.baseMeta(reqCtx, action))
	}
	if member == nil || member.User == nil {
		return t.errorResult(action, "member not found", nil, t.baseMeta(reqCtx, action))
	}
	display := member.User.GlobalName
	if strings.TrimSpace(display) == "" {
		display = member.User.Username
	}

	data := map[string]any{
		"user_id":      member.User.ID,
		"username":     member.User.Username,
		"display_name": display,
		"nick":         member.Nick,
		"bot":          member.User.Bot,
		"roles":        member.Roles,
		"joined_at":    member.JoinedAt,
	}
	meta := t.baseMeta(reqCtx, action)
	meta["user_id"] = userID
	return encodeObserveResult(data, meta, nil, "", false), nil
}

func (t *DiscordObserveTool) listPresences(reqCtx RequestContext, action string) (domain.ToolResult, error) {
	presences, err := t.runtime.GuildPresences(reqCtx.GuildID)
	if err != nil {
		if errors.Is(err, discordruntime.ErrMissingPresenceIntent) {
			metricLogf("discord_tool.degraded_missing_intent", &discordToolDegradedMissingIntents)
			meta := t.baseMeta(reqCtx, action)
			meta["count"] = 0
			warnings := []string{
				"Missing privileged intent: enable discord_presence_intent_enabled and restart bot to use list_presences.",
			}
			return encodeObserveResult([]map[string]any{}, meta, warnings, "", false), nil
		}
		return t.errorResult(action, "list presences failed: "+err.Error(), nil, t.baseMeta(reqCtx, action))
	}

	items := make([]map[string]any, 0, len(presences))
	for _, presence := range presences {
		if presence == nil {
			continue
		}
		userID := ""
		if presence.User != nil {
			userID = presence.User.ID
		}
		activities := make([]map[string]any, 0, len(presence.Activities))
		for _, act := range presence.Activities {
			if act == nil {
				continue
			}
			activities = append(activities, map[string]any{
				"name": act.Name,
				"type": fmt.Sprintf("%d", int(act.Type)),
				"url":  act.URL,
			})
		}
		items = append(items, map[string]any{
			"user_id":    userID,
			"status":     string(presence.Status),
			"activities": activities,
		})
	}

	meta := t.baseMeta(reqCtx, action)
	meta["count"] = len(items)
	return encodeObserveResult(items, meta, nil, "", false), nil
}

func (t *DiscordObserveTool) listMessages(ctx context.Context, reqCtx RequestContext, action, requestedChannelID, before string, limit int) (domain.ToolResult, error) {
	channelID, err := t.resolveMessageChannel(ctx, reqCtx, requestedChannelID)
	if err != nil {
		return t.errorResult(action, err.Error(), nil, t.baseMeta(reqCtx, action))
	}

	msgs, err := t.runtime.ChannelMessages(ctx, channelID, limit, before)
	if err != nil {
		return t.errorResult(action, "list messages failed: "+err.Error(), nil, t.baseMeta(reqCtx, action))
	}

	items := make([]map[string]any, 0, len(msgs))
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		attachments := make([]map[string]any, 0, len(msg.Attachments))
		for _, att := range msg.Attachments {
			if att == nil {
				continue
			}
			attachments = append(attachments, map[string]any{
				"id":           att.ID,
				"filename":     att.Filename,
				"content_type": att.ContentType,
				"size":         att.Size,
				"url":          att.URL,
			})
		}
		author := map[string]any{}
		if msg.Author != nil {
			display := msg.Author.GlobalName
			if strings.TrimSpace(display) == "" {
				display = msg.Author.Username
			}
			author["id"] = msg.Author.ID
			author["username"] = msg.Author.Username
			author["display_name"] = display
			author["bot"] = msg.Author.Bot
		}
		replyTo := ""
		if msg.MessageReference != nil {
			replyTo = msg.MessageReference.MessageID
		}

		items = append(items, map[string]any{
			"id":          msg.ID,
			"content":     msg.Content,
			"timestamp":   msg.Timestamp,
			"author":      author,
			"attachments": attachments,
			"reply_to":    replyTo,
		})
	}

	meta := t.baseMeta(reqCtx, action)
	meta["channel_id"] = channelID
	meta["limit"] = limit
	meta["count"] = len(items)
	meta["before"] = before
	meta["has_more"] = len(items) >= limit
	meta["message_scope"] = t.runtime.Options().MessageScope
	return encodeObserveResult(items, meta, nil, "", false), nil
}

func (t *DiscordObserveTool) resolveMessageChannel(ctx context.Context, reqCtx RequestContext, requestedChannelID string) (string, error) {
	channelID := strings.TrimSpace(requestedChannelID)
	if channelID == "" {
		channelID = strings.TrimSpace(reqCtx.ChannelID)
	}
	if channelID == "" {
		return "", fmt.Errorf("channel_id is required for list_messages")
	}

	channel, err := t.runtime.Channel(ctx, channelID)
	if err != nil {
		return "", fmt.Errorf("resolve channel failed: %w", err)
	}
	if channel.Type == discordgo.ChannelTypeDM || channel.Type == discordgo.ChannelTypeGroupDM {
		metricLogf("discord_tool.denied_dm", &discordToolDeniedDM)
		return "", fmt.Errorf("dm channel access is not allowed")
	}
	if strings.TrimSpace(channel.GuildID) == "" {
		metricLogf("discord_tool.denied_dm", &discordToolDeniedDM)
		return "", fmt.Errorf("channel does not belong to a guild")
	}
	if channel.GuildID != reqCtx.GuildID {
		metricLogf("discord_tool.denied_cross_guild", &discordToolDeniedCrossGuild)
		return "", fmt.Errorf("cross-guild access denied")
	}

	if t.runtime.Options().MessageScope == discordruntime.ObserveScopeCurrentChannel {
		if reqCtx.ChannelID == "" {
			return "", fmt.Errorf("request context missing channel_id for current_channel scope")
		}
		if channel.ID != reqCtx.ChannelID {
			return "", fmt.Errorf("message scope is current_channel; only channel %s is allowed", reqCtx.ChannelID)
		}
	}

	return channel.ID, nil
}

func (t *DiscordObserveTool) parseLimit(input map[string]interface{}) int {
	limit := t.runtime.Options().MaxResults
	if limit <= 0 {
		limit = 25
	}
	if input == nil {
		return limit
	}
	raw, ok := input["limit"]
	if !ok {
		return limit
	}
	switch v := raw.(type) {
	case int:
		limit = v
	case int32:
		limit = int(v)
	case int64:
		limit = int(v)
	case float64:
		limit = int(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			limit = int(n)
		}
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	if limit <= 0 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}
	maxAllowed := t.runtime.Options().MaxResults
	if maxAllowed > 0 && limit > maxAllowed {
		limit = maxAllowed
	}
	return limit
}

func (t *DiscordObserveTool) baseMeta(reqCtx RequestContext, action string) map[string]any {
	return map[string]any{
		"action":     action,
		"source":     reqCtx.Source,
		"guild_id":   reqCtx.GuildID,
		"channel_id": reqCtx.ChannelID,
		"user_id":    reqCtx.UserID,
	}
}

func (t *DiscordObserveTool) errorResult(action, message string, warnings []string, meta map[string]any) (domain.ToolResult, error) {
	if meta == nil {
		meta = map[string]any{}
	}
	if _, ok := meta["action"]; !ok && action != "" {
		meta["action"] = action
	}
	return encodeObserveResult(nil, meta, warnings, message, true), nil
}

func encodeObserveResult(data any, meta map[string]any, warnings []string, errMsg string, isError bool) domain.ToolResult {
	payload := map[string]any{
		"data":     data,
		"meta":     meta,
		"warnings": warnings,
	}
	if errMsg != "" {
		payload["error"] = errMsg
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return domain.ToolResult{
			Content: fmt.Sprintf(`{"error":"encode discord_observe result failed: %s"}`, sanitizeJSONError(err.Error())),
			IsError: true,
		}
	}
	return domain.ToolResult{
		Content: string(b),
		IsError: isError,
	}
}

func channelTypeName(t discordgo.ChannelType) string {
	switch t {
	case discordgo.ChannelTypeGuildText:
		return "guild_text"
	case discordgo.ChannelTypeDM:
		return "dm"
	case discordgo.ChannelTypeGuildVoice:
		return "guild_voice"
	case discordgo.ChannelTypeGroupDM:
		return "group_dm"
	case discordgo.ChannelTypeGuildCategory:
		return "guild_category"
	default:
		return fmt.Sprintf("type_%d", int(t))
	}
}

func sanitizeJSONError(message string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ").Replace(message)
}

func readString(input map[string]interface{}, key string) string {
	if input == nil {
		return ""
	}
	v, ok := input[key]
	if !ok {
		return ""
	}
	switch raw := v.(type) {
	case string:
		return strings.TrimSpace(raw)
	case json.Number:
		return strings.TrimSpace(raw.String())
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", raw))
	}
}

func metricLogf(metric string, counter *uint64) {
	if counter == nil {
		return
	}
	value := atomic.AddUint64(counter, 1)
	logTool.Logf("metric %s=%d", metric, value)
}

func summarizeDiscordID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "-"
	}
	return truncateObserveLogField(id, 28)
}

func truncateObserveLogField(text string, maxLen int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "-"
	}
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")
	if maxLen <= 0 || len(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return text[:maxLen]
	}
	return text[:maxLen-3] + "..."
}

func logObserveCompletion(action string, result domain.ToolResult, execErr error) {
	status := "ok"
	if execErr != nil || result.IsError {
		status = "error"
	}

	summary := summarizeObserveResultForLog(result)
	if execErr != nil {
		logTool.Warnf(
			"discord_observe done: action=%s status=%s exec_error=%s result=%s",
			action,
			status,
			truncateObserveLogField(execErr.Error(), 180),
			summary,
		)
		return
	}
	if result.IsError {
		logTool.Warnf("discord_observe done: action=%s status=%s result=%s", action, status, summary)
		return
	}
	logTool.Logf("discord_observe done: action=%s status=%s result=%s", action, status, summary)
}

func summarizeObserveResultForLog(result domain.ToolResult) string {
	parts := []string{
		fmt.Sprintf("is_error=%t", result.IsError),
		fmt.Sprintf("chars=%d", len(result.Content)),
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		return strings.Join(parts, " ")
	}

	if meta, ok := payload["meta"].(map[string]any); ok {
		if count, exists := meta["count"]; exists {
			parts = append(parts, fmt.Sprintf("count=%v", count))
		}
		if channel, exists := meta["channel_id"]; exists {
			channelText := truncateObserveLogField(fmt.Sprintf("%v", channel), 28)
			if channelText != "-" {
				parts = append(parts, fmt.Sprintf("channel=%s", channelText))
			}
		}
		if limit, exists := meta["limit"]; exists {
			parts = append(parts, fmt.Sprintf("limit=%v", limit))
		}
	}

	if warnings, ok := payload["warnings"].([]any); ok && len(warnings) > 0 {
		parts = append(parts, fmt.Sprintf("warnings=%d", len(warnings)))
	}
	if errMsg, ok := payload["error"].(string); ok && strings.TrimSpace(errMsg) != "" {
		parts = append(parts, fmt.Sprintf("error=%s", truncateObserveLogField(errMsg, 120)))
	}

	return strings.Join(parts, " ")
}
