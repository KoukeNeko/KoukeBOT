package bootstrap

import (
	"os"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

func ResolvePort(defaultPort string) string {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}
	return port
}

func ResolveSettings(settingsStore *config.SettingsStore, logs Loggers) config.Settings {
	settings, err := settingsStore.Load()
	if err != nil {
		logs.System.Warnf("failed to load settings: %v", err)
	}

	if envKey := os.Getenv("GEMINI_API_KEY"); envKey != "" {
		settings.GeminiAPIKey = envKey
		settings.ProviderType = config.ProviderAPIKey
		settings.ProviderID = llm.ProviderGeminiAPIKey
	}
	if envModel := os.Getenv("GEMINI_MODEL"); envModel != "" {
		settings.GeminiModel = llm.NormalizeGoogleModelAlias(envModel)
		settings.ProviderID, settings.GeminiModel, settings.ModelID = llm.ResolveProviderAndModel(
			settings.ProviderID,
			settings.ModelID,
			settings.GeminiModel,
		)
	}
	if envToken := os.Getenv("DISCORD_TOKEN"); envToken != "" {
		settings.DiscordToken = envToken
	}

	return settings
}

func LoadPersonaService(baseDir string, logs Loggers) (*app.PersonaService, error) {
	personaService, err := app.NewPersonaService(baseDir)
	if err != nil {
		return nil, err
	}
	logs.Persona.Logf("loaded %d persona(s)", len(personaService.GetAll()))
	for id, p := range personaService.GetAll() {
		logs.Persona.Logf("  %s [%s]", p.Config.Meta.Name, id)
	}
	return personaService, nil
}
