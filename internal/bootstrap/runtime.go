package bootstrap

import (
	"context"
	"fmt"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	schedulerpkg "github.com/KoukeNeko/KoukeBOT/internal/app/scheduler"
	"github.com/KoukeNeko/KoukeBOT/internal/app/tooling"
	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	discordbot "github.com/KoukeNeko/KoukeBOT/internal/infra/discord"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/mcp"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

type Loggers struct {
	System  *logger.Module
	Persona *logger.Module
	LLM     *logger.Module
	Store   *logger.Module
	Tools   *logger.Module
	Discord *logger.Module
	HTTP    *logger.Module
}

type Paths struct {
	PersonasDir         string
	ConversationsDir    string
	ImagesDir           string
	ScheduledTasksPath  string
	OAuthTokenPath      string
	OAuthPoolPath       string
	AIStudioSessionPath string
	WebDistDir          string
}

type RuntimeServices struct {
	ProviderManager  *app.ProviderManager
	ChatService      *app.ChatService
	SchedulerService *schedulerpkg.Service
	ConvStore        store.ConversationRepository
	ImageStore       *store.ImageStore
	DiscordRuntime   *discordbot.Runtime
	MCPManager       *mcp.Manager
}

func BuildRuntimeServices(
	ctx context.Context,
	settings config.Settings,
	port string,
	defaultModel string,
	personaService *app.PersonaService,
	paths Paths,
	logs Loggers,
) (*RuntimeServices, error) {
	providerManager := app.NewProviderManager(nil)
	providerID, model, _ := llm.ResolveProviderAndModel(settings.ProviderID, settings.ModelID, settings.GeminiModel)
	if model == "" {
		model = defaultModel
	}
	initProvider(ctx, settings, providerManager, providerID, model, port, paths, logs)

	convStore, err := store.NewFileStore(paths.ConversationsDir)
	if err != nil {
		return nil, fmt.Errorf("init conversation store: %w", err)
	}
	logs.Store.Log(paths.ConversationsDir)

	imageStore, err := store.NewImageStore(paths.ImagesDir)
	if err != nil {
		return nil, fmt.Errorf("init image store: %w", err)
	}
	logs.Store.Log(paths.ImagesDir)

	discordRuntime := discordbot.NewRuntime(discordbot.RuntimeOptions{
		MemberIntentEnabled:   settings.DiscordMemberIntentEnabled,
		PresenceIntentEnabled: settings.DiscordPresenceIntentEnabled,
		MessageScope:          settings.DiscordObserveMessageScope,
		MaxResults:            settings.DiscordObserveMaxResults,
	})
	scheduledTaskStore, err := store.NewScheduledTaskFileStore(paths.ScheduledTasksPath)
	if err != nil {
		return nil, fmt.Errorf("init scheduled task store: %w", err)
	}
	logs.Store.Log(paths.ScheduledTasksPath)

	schedulerService := schedulerpkg.NewService(scheduledTaskStore, schedulerpkg.ServiceOptions{
		MonitorMaxPerUser: settings.ScheduledMonitorMaxPerUser,
	})
	schedulerService.SetActivitySource(discordRuntimeActivitySource{runtime: discordRuntime})

	mcpManager := mcp.NewManager()
	fileToolPersonaID := tooling.SelectFileToolPersonaID(paths.PersonasDir, settings.DiscordPersonaID)
	catalog := tooling.BuildCatalog(ctx, settings, fileToolPersonaID, discordRuntime, schedulerService, mcpManager)
	tools := catalog.Tools
	for _, name := range catalog.Names {
		logs.Tools.Logf("%s", name)
	}
	for _, warning := range catalog.Warnings {
		logs.Tools.Warnf("%s", warning)
	}
	logs.Tools.Logf("total: %d tool(s) registered", len(tools))
	if len(tools) > 0 {
		providerManager.RegisterTools(tools)
	}

	var contextManager *app.ContextManager
	if settings.ContextFlushEnabled {
		contextManager = app.NewContextManager(settings.ContextFlushThreshold, settings.ContextWindowSize)
		logs.System.Logf(
			"context flush enabled (threshold: %d, window: %d)",
			settings.ContextFlushThreshold,
			settings.ContextWindowSize,
		)
	}

	assembler := app.NewContextAssembler()
	assembler.UpdateGlobalTimezone(settings.Timezone)
	assembler.UpdatePromptGuardPolicy(app.PromptGuardPolicy{
		PersonaLockEveryTurns:     settings.PersonaLockEveryTurns,
		TripletOutputEnabled:      settings.TripletOutputEnabled,
		AnalysisVisibility:        settings.AnalysisVisibility,
		NegativeConstraintEnabled: settings.NegativeConstraintEnabled,
		NegativeConstraintDefault: settings.NegativeConstraintDefault,
	})

	chatService := app.NewChatService(
		providerManager,
		assembler,
		convStore,
		imageStore,
		0,
		personaService.GetAll(),
		tools,
		contextManager,
	)
	chatService.UpdateToolExecutionPolicy(app.ToolExecutionPolicy{
		ToolGuardEnabled:               settings.ToolGuardEnabled,
		MaxToolRounds:                  settings.MaxToolRounds,
		MaxWebSearchCallsPerTurn:       settings.MaxWebSearchCallsPerTurn,
		ForceFinalAnswerAfterToolLimit: settings.ForceFinalAnswerAfterToolLimit,
		DiscordShowInternalToolErrors:  settings.DiscordShowInternalToolErrors,
	})
	chatService.UpdateMemoryRetentionPolicy(app.MemoryRetentionPolicy{
		RecentRounds: settings.MemoryRecentRounds,
	})
	schedulerService.SetMessageGenerator(chatService)

	return &RuntimeServices{
		ProviderManager:  providerManager,
		ChatService:      chatService,
		SchedulerService: schedulerService,
		ConvStore:        convStore,
		ImageStore:       imageStore,
		DiscordRuntime:   discordRuntime,
		MCPManager:       mcpManager,
	}, nil
}

type discordRuntimeActivitySource struct {
	runtime *discordbot.Runtime
}

func (s discordRuntimeActivitySource) ListChannelMessages(ctx context.Context, channelID, before string, limit int) ([]domain.ScheduledActivityMessage, error) {
	if s.runtime == nil {
		return nil, fmt.Errorf("discord runtime is not configured")
	}
	msgs, err := s.runtime.ChannelMessages(ctx, channelID, limit, before)
	if err != nil {
		return nil, err
	}
	out := make([]domain.ScheduledActivityMessage, 0, len(msgs))
	for _, msg := range msgs {
		if msg == nil || msg.Author == nil {
			continue
		}
		out = append(out, domain.ScheduledActivityMessage{
			ID:        msg.ID,
			AuthorID:  msg.Author.ID,
			Content:   msg.Content,
			Timestamp: msg.Timestamp.UTC(),
		})
	}
	return out, nil
}
