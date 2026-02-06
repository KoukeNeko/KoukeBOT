package persona

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"gopkg.in/yaml.v3"
)

const (
	configFileName  = "config.yaml"
	loreFileName    = "lore.md"
	anchorsFileName = "anchors.yaml"
)

// LoadPersona reads a persona folder and assembles a PersonaModule.
// baseDir: root personas directory (e.g., "configs/personas")
// id: persona folder name (e.g., "hayasaka")
func LoadPersona(baseDir, id string) (*domain.PersonaModule, error) {
	personaDir := filepath.Join(baseDir, id)

	config, err := loadConfig(filepath.Join(personaDir, configFileName))
	if err != nil {
		return nil, fmt.Errorf("load config for persona %q: %w", id, err)
	}

	lore, err := loadLore(filepath.Join(personaDir, loreFileName))
	if err != nil {
		return nil, fmt.Errorf("load lore for persona %q: %w", id, err)
	}

	anchors, err := loadAnchors(filepath.Join(personaDir, anchorsFileName))
	if err != nil {
		return nil, fmt.Errorf("load anchors for persona %q: %w", id, err)
	}

	return &domain.PersonaModule{
		Config:  *config,
		Lore:    lore,
		Anchors: anchors,
	}, nil
}

// LoadAllPersonas scans baseDir for persona folders and loads each one.
func LoadAllPersonas(baseDir string) (map[string]*domain.PersonaModule, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, fmt.Errorf("read personas directory %q: %w", baseDir, err)
	}

	personas := make(map[string]*domain.PersonaModule, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		module, err := LoadPersona(baseDir, entry.Name())
		if err != nil {
			return nil, err
		}
		personas[module.Config.Meta.ID] = module
	}

	return personas, nil
}

func loadConfig(path string) (*domain.PersonaConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg domain.PersonaConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	return &cfg, nil
}

func loadLore(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), nil
}

func loadAnchors(path string) ([]domain.Anchor, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var file domain.AnchorFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	return file.Anchors, nil
}

// SavePersona writes the persona module back to disk:
// config.yaml, lore.md, and anchors.yaml.
func SavePersona(baseDir, id string, module *domain.PersonaModule) error {
	personaDir := filepath.Join(baseDir, id)

	if err := saveConfig(filepath.Join(personaDir, configFileName), &module.Config); err != nil {
		return fmt.Errorf("save config for persona %q: %w", id, err)
	}

	if err := saveLore(filepath.Join(personaDir, loreFileName), module.Lore); err != nil {
		return fmt.Errorf("save lore for persona %q: %w", id, err)
	}

	if err := saveAnchors(filepath.Join(personaDir, anchorsFileName), module.Anchors); err != nil {
		return fmt.Errorf("save anchors for persona %q: %w", id, err)
	}

	return nil
}

func saveConfig(path string, cfg *domain.PersonaConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

func saveLore(path, lore string) error {
	return os.WriteFile(path, []byte(lore), 0o644)
}

func saveAnchors(path string, anchors []domain.Anchor) error {
	file := domain.AnchorFile{Anchors: anchors}
	data, err := yaml.Marshal(&file)
	if err != nil {
		return fmt.Errorf("marshal anchors: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}
