package domain

// PersonaMeta holds identification metadata for a persona.
type PersonaMeta struct {
	ID          string `yaml:"id" json:"id"`
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
	Version     string `yaml:"version" json:"version"`
	Author      string `yaml:"author" json:"author"`
}

// GenerationConfig controls LLM generation parameters.
type GenerationConfig struct {
	Temperature      float64 `yaml:"temperature" json:"temperature"`
	MaxTokens        int     `yaml:"max_tokens" json:"max_tokens"`
	TopP             float64 `yaml:"top_p" json:"top_p"`
	FrequencyPenalty float64 `yaml:"frequency_penalty" json:"frequency_penalty"`
	PresencePenalty  float64 `yaml:"presence_penalty" json:"presence_penalty"`
}

// PersonaConfig maps to config.yaml — the "brain" of a persona.
type PersonaConfig struct {
	Meta           PersonaMeta      `yaml:"meta" json:"meta"`
	SystemTemplate string           `yaml:"system_template" json:"system_template"`
	Generation     GenerationConfig `yaml:"generation" json:"generation"`
	Variables      map[string]any   `yaml:"variables" json:"variables"`
}

// Anchor represents a single few-shot example for style anchoring.
type Anchor struct {
	ID          string   `yaml:"id" json:"id"`
	Tags        []string `yaml:"tags" json:"tags"`
	UserInput   string   `yaml:"user_input" json:"user_input"`
	BotResponse string   `yaml:"bot_response" json:"bot_response"`
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
