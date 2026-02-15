package api

import (
	"fmt"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
)

func validatePromptGuardSettings(lockEvery int, analysisVisibility string, negativeConstraintEnabled bool, negativeConstraintDefault string) error {
	if lockEvery < 1 || lockEvery > 20 {
		return fmt.Errorf("persona_lock_every_turns must be between 1 and 20")
	}
	switch analysisVisibility {
	case "visible", "hidden", "debug":
	default:
		return fmt.Errorf("analysis_visibility must be one of: visible, hidden, debug")
	}
	if negativeConstraintEnabled && strings.TrimSpace(negativeConstraintDefault) == "" {
		return fmt.Errorf("negative_constraint_default cannot be empty when negative_constraint_enabled=true")
	}
	return nil
}

func validateDiscordObserveSettings(maxResults int, messageScope string) error {
	if maxResults < 1 || maxResults > 100 {
		return fmt.Errorf("discord_observe_max_results must be between 1 and 100")
	}
	switch messageScope {
	case config.DiscordObserveScopeCurrentChannel, config.DiscordObserveScopeCurrentGuild:
		return nil
	default:
		return fmt.Errorf(
			"discord_observe_message_scope must be one of: %s, %s",
			config.DiscordObserveScopeCurrentChannel,
			config.DiscordObserveScopeCurrentGuild,
		)
	}
}

func validateToolLoopSettings(maxToolRounds, maxWebSearchCallsPerTurn, transientRecoveryWaitMaxSeconds int) error {
	if maxToolRounds < 1 || maxToolRounds > 6 {
		return fmt.Errorf("max_tool_rounds must be between 1 and 6")
	}
	if maxWebSearchCallsPerTurn < 1 || maxWebSearchCallsPerTurn > 4 {
		return fmt.Errorf("max_web_search_calls_per_turn must be between 1 and 4")
	}
	if transientRecoveryWaitMaxSeconds < 1 || transientRecoveryWaitMaxSeconds > 15 {
		return fmt.Errorf("transient_recovery_wait_max_seconds must be between 1 and 15")
	}
	return nil
}

func validateSchedulerSettings(scheduledMonitorMaxPerUser int) error {
	if scheduledMonitorMaxPerUser < 1 || scheduledMonitorMaxPerUser > config.MaxScheduledMonitorMaxPerUser {
		return fmt.Errorf(
			"scheduled_monitor_max_per_user must be between 1 and %d",
			config.MaxScheduledMonitorMaxPerUser,
		)
	}
	return nil
}

func validateMemoryRetentionSettings(memoryRecentRounds int) error {
	if memoryRecentRounds < config.MinMemoryRecentRounds || memoryRecentRounds > config.MaxMemoryRecentRounds {
		return fmt.Errorf(
			"memory_recent_rounds must be between %d and %d",
			config.MinMemoryRecentRounds,
			config.MaxMemoryRecentRounds,
		)
	}
	return nil
}
