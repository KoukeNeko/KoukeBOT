package bootstrap

import (
	"fmt"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	schedulerpkg "github.com/KoukeNeko/KoukeBOT/internal/app/scheduler"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	discordbot "github.com/KoukeNeko/KoukeBOT/internal/infra/discord"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
	discordhandler "github.com/KoukeNeko/KoukeBOT/internal/interface/discord"
)

func MaybeStartDiscordBot(settings config.Settings, personaService *app.PersonaService, runtime *RuntimeServices, logs Loggers) *discordbot.Bot {
	if settings.DiscordToken == "" {
		logs.Discord.Warnf("DISCORD_TOKEN not set — skipped")
		return nil
	}

	bot, err := startDiscordBot(
		settings.DiscordToken,
		settings.DiscordUseReply,
		settings.DiscordStripLeadingBotMention,
		settings.DiscordPersonaID,
		settings.DiscordMemberIntentEnabled,
		settings.DiscordPresenceIntentEnabled,
		personaService,
		runtime.ChatService,
		runtime.ConvStore,
		runtime.ImageStore,
		runtime.ProviderManager,
		runtime.DiscordRuntime,
		runtime.SchedulerService,
		logs,
	)
	if err != nil {
		logs.Discord.Warnf("failed to start: %v", err)
		logs.Discord.Log("continuing without Discord — HTTP API still available")
		return nil
	}
	if settings.LogChannelID != "" {
		logger.SetDiscordOutput(bot, settings.LogChannelID)
		logs.System.Logf("stdout → Discord channel %s", settings.LogChannelID)
	}
	return bot
}

func startDiscordBot(
	token string,
	useReply bool,
	stripLeadingBotMentionPrefix bool,
	preferredPersonaID string,
	enableMemberIntent bool,
	enablePresenceIntent bool,
	personaService *app.PersonaService,
	chatService *app.ChatService,
	convStore store.ConversationRepository,
	imageStore *store.ImageStore,
	providerManager *app.ProviderManager,
	discordRuntime *discordbot.Runtime,
	schedulerService *schedulerpkg.Service,
	logs Loggers,
) (*discordbot.Bot, error) {
	var personaID string
	if preferredPersonaID != "" {
		if _, ok := personaService.Get(preferredPersonaID); ok {
			personaID = preferredPersonaID
		}
	}
	if personaID == "" {
		for id := range personaService.GetAll() {
			personaID = id
			break
		}
	}

	if personaID == "" {
		return nil, fmt.Errorf("no personas loaded for Discord bot")
	}

	bot, err := discordbot.NewBot(token, personaID, discordbot.NewBotOptions{
		EnableMemberIntent:   enableMemberIntent,
		EnablePresenceIntent: enablePresenceIntent,
	})
	if err != nil {
		return nil, err
	}

	handler := discordhandler.NewHandler(bot, chatService, schedulerService, convStore, useReply, stripLeadingBotMentionPrefix)
	handler.Register()

	if err := bot.Start(); err != nil {
		return nil, err
	}
	if discordRuntime != nil {
		discordRuntime.BindSession(bot.Session())
	}

	slashCmds := discordhandler.NewSlashCommands(bot, personaService, chatService, convStore, imageStore, providerManager)
	if err := slashCmds.Register(); err != nil {
		return nil, fmt.Errorf("register slash commands: %w", err)
	}

	logs.Discord.Logf("serving persona: %s", personaID)
	return bot, nil
}
