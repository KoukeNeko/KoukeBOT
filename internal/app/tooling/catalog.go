package tooling

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	schedulerpkg "github.com/KoukeNeko/KoukeBOT/internal/app/scheduler"
	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	discordruntime "github.com/KoukeNeko/KoukeBOT/internal/infra/discord"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/mcp"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/search"
)

type BuildCatalogResult struct {
	Tools    []domain.Tool
	Names    []string
	Warnings []string
}

func BuildCatalog(
	ctx context.Context,
	settings config.Settings,
	personaID string,
	discordRuntime *discordruntime.Runtime,
	schedulerService *schedulerpkg.Service,
	mcpManager *mcp.Manager,
) BuildCatalogResult {
	var tools []domain.Tool
	var names []string
	var warnings []string

	// Web Search Tool
	if settings.WebSearchEnabled {
		router := search.NewProviderRouter(
			settings.BraveAPIKey,
			settings.PerplexityAPIKey,
			settings.SearchProvider,
		)
		if router.IsConfigured() {
			tools = append(tools, llm.NewWebSearchTool(router))
			names = append(names, fmt.Sprintf("web_search (provider: %s)", settings.SearchProvider))
		}
	}

	// Web Fetch Tool
	if settings.WebFetchEnabled {
		tools = append(tools, llm.NewWebFetchTool(search.NewWebFetcher()))
		names = append(names, "web_fetch")
	}

	// Session Status Tool
	timezone := settings.Timezone
	if timezone == "" {
		timezone = "auto"
	}
	timeFormat := settings.TimeFormat
	if timeFormat == "" {
		timeFormat = "auto"
	}
	tools = append(tools, llm.NewSessionStatusTool(timezone, timeFormat))
	names = append(names, fmt.Sprintf("session_status (tz: %s)", timezone))

	// Discord observe tool (read-only; Discord-source only).
	if settings.DiscordObserveEnabled && discordRuntime != nil {
		discordRuntime.SetOptions(discordruntime.RuntimeOptions{
			MemberIntentEnabled:   settings.DiscordMemberIntentEnabled,
			PresenceIntentEnabled: settings.DiscordPresenceIntentEnabled,
			MessageScope:          settings.DiscordObserveMessageScope,
			MaxResults:            settings.DiscordObserveMaxResults,
		})
		tools = append(tools, llm.NewDiscordObserveTool(discordRuntime))
		names = append(names, "discord_observe (read-only; current guild scope)")
	}

	// Scheduler tool (Discord source only).
	if schedulerService != nil {
		tools = append(tools, llm.NewSchedulerTool(schedulerService))
		names = append(names, "schedule_manager (discord only)")
	}

	// File operations declarations only (runtime persona-bound execution).
	declarationPersonaID := personaID
	if declarationPersonaID == "" {
		declarationPersonaID = "_template"
	}
	declarationDir := filepath.Join("configs/personas", declarationPersonaID)
	tools = append(tools,
		llm.NewFileReadTool(declarationDir),
		llm.NewFileWriteTool(declarationDir),
		llm.NewFileAppendTool(declarationDir),
	)
	names = append(names, "file_read, file_write, file_append (declarations only; runtime persona-bound execution enabled)")

	if mcpManager != nil {
		mcpTools, mcpWarnings := mcpManager.BuildTools(ctx, settings.MCP)
		tools = append(tools, mcpTools...)
		warnings = append(warnings, mcpWarnings...)
		for _, tool := range mcpTools {
			names = append(names, tool.Name())
		}
	} else if settings.MCP.Enabled {
		warnings = append(warnings, "MCP is enabled but manager is not configured")
	}

	return BuildCatalogResult{
		Tools:    tools,
		Names:    names,
		Warnings: warnings,
	}
}

func SelectFileToolPersonaID(baseDir, preferred string) string {
	if preferred != "" {
		path := filepath.Join(baseDir, preferred)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return preferred
		}
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		return entry.Name()
	}
	return ""
}
