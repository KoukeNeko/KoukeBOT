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

// StyleGuardConfig controls persona-specific style stability constraints.
type StyleGuardConfig struct {
	NegativeConstraints string   `yaml:"negative_constraints,omitempty" json:"negative_constraints,omitempty"`
	SignatureTerms      []string `yaml:"signature_terms,omitempty" json:"signature_terms,omitempty"`
}

// PersonaConfig maps to config.yaml — the "brain" of a persona.
type PersonaConfig struct {
	Meta           PersonaMeta      `yaml:"meta" json:"meta"`
	SystemTemplate string           `yaml:"system_template" json:"system_template"`
	Generation     GenerationConfig `yaml:"generation" json:"generation"`
	Variables      map[string]any   `yaml:"variables" json:"variables"`
	StyleGuard     StyleGuardConfig `yaml:"style_guard,omitempty" json:"style_guard,omitempty"`

	// Legacy per-persona time/location fields retained for config compatibility.
	// Runtime timezone/timeformat/location now use global system settings.
	Timezone   string `yaml:"timezone,omitempty" json:"timezone,omitempty"`       // "Asia/Taipei", "America/New_York", "auto"
	TimeFormat string `yaml:"time_format,omitempty" json:"time_format,omitempty"` // "auto", "12", "24"
	Location   string `yaml:"location,omitempty" json:"location,omitempty"`       // Optional: explicit location "台北, 台灣"
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
// Assembled from config.yaml + lore.md + anchors.yaml + user.md + memory/.
type PersonaModule struct {
	DirName        string // folder name under configs/personas/ (e.g., "hayasaka")
	Config         PersonaConfig
	Lore           string   // raw markdown from lore.md
	UserProfile    string   // raw markdown from user.md
	LongTermMemory string   // raw markdown from memory/MEMORY.md
	DailyMemory    string   // raw markdown from memory/YYYY-MM-DD.md (today's log)
	Anchors        []Anchor // parsed from anchors.yaml
}
