package bootstrap

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	discordbot "github.com/KoukeNeko/KoukeBOT/internal/infra/discord"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/interface/api"
)

func StartAPIServer(
	ctx context.Context,
	port string,
	personaService *app.PersonaService,
	runtime *RuntimeServices,
	settingsStore *config.SettingsStore,
	discordBot *discordbot.Bot,
	oauthFlow *llm.GeminiOAuthFlow,
	paths Paths,
	logs Loggers,
) <-chan error {
	router := api.NewRouter(
		personaService,
		runtime.ChatService,
		runtime.SchedulerService,
		settingsStore,
		runtime.ProviderManager,
		runtime.MCPManager,
		runtime.DiscordRuntime,
		discordBot,
		oauthFlow,
		runtime.ConvStore,
		runtime.ImageStore,
		ctx,
	)
	handler := router.Handler()

	if info, err := os.Stat(paths.WebDistDir); err == nil && info.IsDir() {
		handler = withSPA(paths.WebDistDir, handler)
		logs.HTTP.Logf("frontend: %s", paths.WebDistDir)
	}

	errCh := make(chan error, 1)
	go func() {
		logs.HTTP.Logf("listening on :%s", port)
		if err := http.ListenAndServe(":"+port, handler); err != nil && err != http.ErrServerClosed {
			logs.HTTP.Errorf("server error: %v", err)
			errCh <- err
		}
		close(errCh)
	}()
	return errCh
}

func withSPA(webDir string, apiHandler http.Handler) http.Handler {
	fs := http.FileServer(http.Dir(webDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			apiHandler.ServeHTTP(w, r)
			return
		}

		filePath := filepath.Join(webDir, r.URL.Path)
		if _, err := os.Stat(filePath); err == nil {
			fs.ServeHTTP(w, r)
			return
		}

		http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
	})
}
