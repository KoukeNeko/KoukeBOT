package persona

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"gopkg.in/yaml.v3"
)

const (
	configFileName      = "config.yaml"
	loreFileName        = "lore.md"
	userProfileFileName = "user.md"
	anchorsFileName     = "anchors.yaml"
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

	userProfile, err := loadUserProfile(filepath.Join(personaDir, userProfileFileName))
	if err != nil {
		return nil, fmt.Errorf("load user profile for persona %q: %w", id, err)
	}

	anchors, err := loadAnchors(filepath.Join(personaDir, anchorsFileName))
	if err != nil {
		return nil, fmt.Errorf("load anchors for persona %q: %w", id, err)
	}

	return &domain.PersonaModule{
		Config:      *config,
		Lore:        lore,
		UserProfile: userProfile,
		Anchors:     anchors,
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

func loadUserProfile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil // Optional
	}
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

	if err := saveUserProfile(filepath.Join(personaDir, userProfileFileName), module.UserProfile); err != nil {
		return fmt.Errorf("save user profile for persona %q: %w", id, err)
	}

	if err := saveAnchors(filepath.Join(personaDir, anchorsFileName), module.Anchors); err != nil {
		return fmt.Errorf("save anchors for persona %q: %w", id, err)
	}

	return nil
}

func saveConfig(path string, cfg *domain.PersonaConfig) error {
	return writeYAMLPreservingComments(path, cfg)
}

func saveLore(path, lore string) error {
	return os.WriteFile(path, []byte(lore), 0o644)
}

func saveUserProfile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func saveAnchors(path string, anchors []domain.Anchor) error {
	file := domain.AnchorFile{Anchors: anchors}
	return writeYAMLPreservingComments(path, &file)
}

// writeYAMLPreservingComments marshals v to YAML while preserving
// comments from the existing file at path.
func writeYAMLPreservingComments(path string, v any) error {
	newData, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	existingData, readErr := os.ReadFile(path)
	if readErr != nil {
		return os.WriteFile(path, newData, 0o644)
	}

	var oldDoc, newDoc yaml.Node
	if err := yaml.Unmarshal(existingData, &oldDoc); err != nil {
		return os.WriteFile(path, newData, 0o644)
	}
	if err := yaml.Unmarshal(newData, &newDoc); err != nil {
		return os.WriteFile(path, newData, 0o644)
	}

	mergeComments(&oldDoc, &newDoc)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&newDoc); err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	enc.Close()

	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// mergeComments recursively copies HeadComment, LineComment, FootComment
// from src to dst nodes with matching keys.
func mergeComments(src, dst *yaml.Node) {
	if src == nil || dst == nil {
		return
	}

	dst.HeadComment = src.HeadComment
	dst.LineComment = src.LineComment
	dst.FootComment = src.FootComment

	if src.Kind != dst.Kind {
		return
	}

	switch src.Kind {
	case yaml.DocumentNode:
		for i := 0; i < len(src.Content) && i < len(dst.Content); i++ {
			mergeComments(src.Content[i], dst.Content[i])
		}
	case yaml.MappingNode:
		srcMap := buildNodeMap(src)
		for i := 0; i+1 < len(dst.Content); i += 2 {
			key := dst.Content[i].Value
			if pair, ok := srcMap[key]; ok {
				mergeComments(pair[0], dst.Content[i])   // key node
				mergeComments(pair[1], dst.Content[i+1]) // value node
			}
		}
	case yaml.SequenceNode:
		for i := 0; i < len(src.Content) && i < len(dst.Content); i++ {
			mergeComments(src.Content[i], dst.Content[i])
		}
	}
}

func buildNodeMap(mapping *yaml.Node) map[string][2]*yaml.Node {
	m := make(map[string][2]*yaml.Node, len(mapping.Content)/2)
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		m[mapping.Content[i].Value] = [2]*yaml.Node{
			mapping.Content[i],
			mapping.Content[i+1],
		}
	}
	return m
}
