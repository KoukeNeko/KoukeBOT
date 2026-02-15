package llm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	discordruntime "github.com/KoukeNeko/KoukeBOT/internal/infra/discord"
)

func TestDiscordObserveTool_InputSchema(t *testing.T) {
	tool := NewDiscordObserveTool(discordruntime.NewRuntime(discordruntime.RuntimeOptions{}))
	schema := tool.InputSchema()

	if schema["type"] != "object" {
		t.Fatalf("schema type = %v, want object", schema["type"])
	}

	required, ok := schema["required"].([]string)
	if !ok || len(required) == 0 || required[0] != "action" {
		t.Fatalf("schema required = %#v, want [action]", schema["required"])
	}

	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("schema properties type invalid: %#v", schema["properties"])
	}
	action, ok := props["action"].(map[string]interface{})
	if !ok {
		t.Fatalf("schema action property missing")
	}
	enumValues, ok := action["enum"].([]string)
	if !ok {
		t.Fatalf("schema action enum type invalid: %#v", action["enum"])
	}
	if len(enumValues) < 5 {
		t.Fatalf("schema action enum too short: %#v", enumValues)
	}
}

func TestDiscordObserveTool_DenyNonDiscordSource(t *testing.T) {
	tool := NewDiscordObserveTool(discordruntime.NewRuntime(discordruntime.RuntimeOptions{}))
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "capabilities",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true")
	}
	if !strings.Contains(result.Content, "only available for Discord-origin requests") {
		t.Fatalf("unexpected content: %s", result.Content)
	}
}

func TestDiscordObserveTool_DenyDMContext(t *testing.T) {
	tool := NewDiscordObserveTool(discordruntime.NewRuntime(discordruntime.RuntimeOptions{}))
	ctx := WithRequestContext(context.Background(), RequestContext{
		Source:    RequestSourceDiscord,
		GuildID:   "",
		ChannelID: "123",
		UserID:    "456",
	})

	result, err := tool.Execute(ctx, map[string]interface{}{
		"action": "capabilities",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true")
	}
	if !strings.Contains(result.Content, "disabled in DM context") {
		t.Fatalf("unexpected content: %s", result.Content)
	}
}

func TestDiscordObserveTool_CapabilitiesWithWarnings(t *testing.T) {
	tool := NewDiscordObserveTool(discordruntime.NewRuntime(discordruntime.RuntimeOptions{
		MemberIntentEnabled:   false,
		PresenceIntentEnabled: false,
		MessageScope:          discordruntime.ObserveScopeCurrentGuild,
		MaxResults:            25,
	}))
	ctx := WithRequestContext(context.Background(), RequestContext{
		Source:    RequestSourceDiscord,
		GuildID:   "guild-1",
		ChannelID: "channel-1",
		UserID:    "user-1",
	})

	result, err := tool.Execute(ctx, map[string]interface{}{
		"action": "capabilities",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected IsError=false, got content: %s", result.Content)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data missing: %#v", payload)
	}
	if data["message_scope"] != discordruntime.ObserveScopeCurrentGuild {
		t.Fatalf("message_scope = %v", data["message_scope"])
	}
	if int(data["max_results"].(float64)) != 25 {
		t.Fatalf("max_results = %v", data["max_results"])
	}

	warnings, ok := payload["warnings"].([]interface{})
	if !ok || len(warnings) == 0 {
		t.Fatalf("expected warnings, got: %#v", payload["warnings"])
	}
}

func TestDiscordObserveTool_ParseLimitClamp(t *testing.T) {
	tool := NewDiscordObserveTool(discordruntime.NewRuntime(discordruntime.RuntimeOptions{
		MaxResults: 40,
	}))

	limit := tool.parseLimit(map[string]interface{}{"limit": float64(80)})
	if limit != 40 {
		t.Fatalf("limit = %d, want 40", limit)
	}

	limit = tool.parseLimit(map[string]interface{}{"limit": float64(-1)})
	if limit != 1 {
		t.Fatalf("limit = %d, want 1", limit)
	}
}
