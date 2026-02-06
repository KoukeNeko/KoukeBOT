package domain

// PersonaMeta holds identification metadata for a persona.
type PersonaMeta struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Version     string `yaml:"version"`
	Author      string `yaml:"author"`
}

// GenerationConfig controls LLM generation parameters.
type GenerationConfig struct {
	Temperature      float64 `yaml:"temperature"`
	MaxTokens        int     `yaml:"max_tokens"`
	TopP             float64 `yaml:"top_p"`
	FrequencyPenalty float64 `yaml:"frequency_penalty"`
	PresencePenalty  float64 `yaml:"presence_penalty"`
}

// PersonaConfig maps to config.yaml — the "brain" of a persona.
type PersonaConfig struct {
	Meta           PersonaMeta        `yaml:"meta"`
	SystemTemplate string             `yaml:"system_template"`
	Generation     GenerationConfig   `yaml:"generation"`
	Variables      map[string]any     `yaml:"variables"`
}

// Anchor represents a single few-shot example for style anchoring.
type Anchor struct {
	ID          string   `yaml:"id"`
	Tags        []string `yaml:"tags"`
	UserInput   string   `yaml:"user_input"`
	BotResponse string   `yaml:"bot_response"`
}

// AnchorFile maps to anchors.yaml.
type AnchorFile struct {
	Anchors []Anchor `yaml:"anchors"`
}

// PersonaModule is the fully loaded, runtime-ready persona.
// Assembled from config.yaml + lore.md + anchors.yaml.
type PersonaModule struct {
	Config  PersonaConfig
	Lore    string   // raw markdown from lore.md
	Anchors []Anchor // parsed from anchors.yaml
}
