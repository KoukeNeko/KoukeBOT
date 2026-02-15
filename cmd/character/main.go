package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/KoukeNeko/KoukeBOT/internal/app/tooling"
	"github.com/KoukeNeko/KoukeBOT/internal/bootstrap"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
)

const (
	personasDir        = "configs/personas"
	settingsPath       = "data/settings.json"
	scheduledTasksPath = "data/schedules/tasks.json"
	oauthTokenPath     = "data/gemini_oauth.json"
	oauthPoolPath      = "data/oauth_pool.json"
	defaultModel       = "gemini-2.5-pro"
	defaultPort        = "8080"
)

var (
	logSystem  = logger.New("system", logger.White)
	logPersona = logger.New("persona", logger.Cyan)
	logLLM     = logger.New("llm", logger.Yellow)
	logStore   = logger.New("store", logger.Blue)
	logTools   = logger.New("tools", logger.Green)
	logDiscord = logger.New("discord", logger.Magenta)
	logHTTP    = logger.New("http", logger.Blue)
)

func main() {
	if err := run(); err != nil {
		logSystem.Errorf("fatal: %v", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logs := bootstrap.Loggers{
		System:  logSystem,
		Persona: logPersona,
		LLM:     logLLM,
		Store:   logStore,
		Tools:   logTools,
		Discord: logDiscord,
		HTTP:    logHTTP,
	}
	paths := bootstrap.Paths{
		PersonasDir:        personasDir,
		ConversationsDir:   "data/conversations",
		ImagesDir:          "data/images",
		ScheduledTasksPath: scheduledTasksPath,
		OAuthTokenPath:     oauthTokenPath,
		OAuthPoolPath:      oauthPoolPath,
		WebDistDir:         filepath.Join("web", "dist"),
	}

	port := bootstrap.ResolvePort(defaultPort)
	settingsStore := config.NewSettingsStore(settingsPath)
	settings := bootstrap.ResolveSettings(settingsStore, logs)

	personaService, err := bootstrap.LoadPersonaService(paths.PersonasDir, logs)
	if err != nil {
		return fmt.Errorf("init persona service: %w", err)
	}
	runtime, err := bootstrap.BuildRuntimeServices(ctx, settings, port, defaultModel, personaService, paths, logs)
	if err != nil {
		return err
	}

	discordBot := bootstrap.MaybeStartDiscordBot(settings, personaService, runtime, logs)
	if runtime.SchedulerService != nil {
		if discordBot != nil {
			runtime.SchedulerService.SetNotifier(discordBot)
		}
		runtime.SchedulerService.Start(ctx)
	}
	oauthFlow := llm.NewGeminiOAuthFlow(
		paths.OAuthTokenPath,
		paths.OAuthPoolPath,
		"http://127.0.0.1:"+port+"/api/oauth/gemini/callback",
	)

	serverErrCh := bootstrap.StartAPIServer(
		ctx,
		port,
		personaService,
		runtime,
		settingsStore,
		discordBot,
		oauthFlow,
		paths,
		logs,
	)

	logSystem.Logf("ready — API on :%s", port)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sigCh:
		logSystem.Log("shutting down...")
		cancel()
		return nil
	case err := <-serverErrCh:
		cancel()
		return err
	}
}

// selectFileToolPersonaID keeps test compatibility while delegating to shared tooling.
func selectFileToolPersonaID(baseDir, preferred string) string {
	return tooling.SelectFileToolPersonaID(baseDir, preferred)
}
