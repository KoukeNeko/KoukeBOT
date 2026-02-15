package api

import (
	"fmt"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	schedulerpkg "github.com/KoukeNeko/KoukeBOT/internal/app/scheduler"
	"github.com/KoukeNeko/KoukeBOT/internal/app/tooling"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
)

const personaConfigsBaseDir = "configs/personas"

type SettingsRuntimeApplier struct {
	handlers *Handlers
}

func NewSettingsRuntimeApplier(h *Handlers) *SettingsRuntimeApplier {
	return &SettingsRuntimeApplier{handlers: h}
}

func (a *SettingsRuntimeApplier) SaveAndApply(existing config.Settings) (map[string]string, error) {
	h := a.handlers
	if h == nil {
		return nil, fmt.Errorf("handlers are not configured")
	}

	logAPI.Logf("settings: saving model=%s provider=%s websearch=%v reply=%v",
		existing.GeminiModel, existing.ProviderType, existing.WebSearchEnabled, existing.DiscordUseReply)

	if err := h.settingsStore.Save(existing); err != nil {
		return nil, fmt.Errorf("save settings: %w", err)
	}

	if h.discordPersona != nil && existing.DiscordPersonaID != "" {
		h.discordPersona.SetPersonaID(existing.DiscordPersonaID)
		logAPI.Logf("settings: discord persona switched to %s", existing.DiscordPersonaID)
	}

	h.chatService.UpdatePromptGuardPolicy(app.PromptGuardPolicy{
		PersonaLockEveryTurns:     existing.PersonaLockEveryTurns,
		TripletOutputEnabled:      existing.TripletOutputEnabled,
		AnalysisVisibility:        existing.AnalysisVisibility,
		NegativeConstraintEnabled: existing.NegativeConstraintEnabled,
		NegativeConstraintDefault: existing.NegativeConstraintDefault,
	})
	h.chatService.UpdateToolExecutionPolicy(app.ToolExecutionPolicy{
		MaxToolRounds:                  existing.MaxToolRounds,
		MaxWebSearchCallsPerTurn:       existing.MaxWebSearchCallsPerTurn,
		ForceFinalAnswerAfterToolLimit: existing.ForceFinalAnswerAfterToolLimit,
		DiscordShowInternalToolErrors:  existing.DiscordShowInternalToolErrors,
	})
	h.chatService.UpdateMemoryRetentionPolicy(app.MemoryRetentionPolicy{
		RecentRounds: existing.MemoryRecentRounds,
	})
	if h.schedulerService != nil {
		h.schedulerService.UpdatePolicy(schedulerpkg.Policy{
			MonitorMaxPerUser: existing.ScheduledMonitorMaxPerUser,
		})
	}
	if pool := h.providerManager.GetPool(); pool != nil {
		pool.SetTransientRecoveryWait(
			existing.TransientRecoveryWaitEnabled,
			time.Duration(existing.TransientRecoveryWaitMaxSeconds)*time.Second,
		)
	}

	fileToolPersonaID := tooling.SelectFileToolPersonaID(personaConfigsBaseDir, existing.DiscordPersonaID)
	catalog := tooling.BuildCatalog(h.ctx, existing, fileToolPersonaID, h.discordRuntime, h.schedulerService, h.mcpManager)
	h.providerManager.RegisterTools(catalog.Tools)
	h.chatService.UpdateToolRegistry(catalog.Tools)
	for _, warning := range catalog.Warnings {
		logAPI.Warnf("settings: tool catalog warning: %s", warning)
	}

	response := map[string]string{"status": "saved"}
	if err := h.activateProvider(existing); err != nil {
		logAPI.Warnf("settings: provider activation failed (settings saved): %v", err)
		response["warning"] = err.Error()
	}
	if len(catalog.Warnings) > 0 {
		warningText := fmt.Sprintf("MCP warnings: %s", strings.Join(catalog.Warnings, "; "))
		if existingWarning := response["warning"]; existingWarning != "" {
			response["warning"] = existingWarning + " | " + warningText
		} else {
			response["warning"] = warningText
		}
	}

	logAPI.Logf("settings: saved (warning=%v)", response["warning"] != "")
	return response, nil
}
