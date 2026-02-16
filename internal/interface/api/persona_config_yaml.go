package api

import (
	"fmt"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"gopkg.in/yaml.v3"
)

var requiredPersonaConfigKeys = []string{
	"meta",
	"system_template",
	"generation",
	"variables",
}

func parseAndValidatePersonaConfigYAML(raw string) (domain.PersonaConfig, error) {
	var cfg domain.PersonaConfig

	if strings.TrimSpace(raw) == "" {
		return cfg, fmt.Errorf("config_yaml cannot be empty")
	}

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return cfg, fmt.Errorf("config_yaml must be valid YAML: %w", err)
	}

	root, err := rootMappingNode(&doc)
	if err != nil {
		return cfg, err
	}

	for _, key := range requiredPersonaConfigKeys {
		value, ok := mappingValueByKey(root, key)
		if !ok {
			return cfg, fmt.Errorf("config_yaml missing required key: %s", key)
		}

		switch key {
		case "meta", "generation", "variables":
			if value.Kind != yaml.MappingNode {
				return cfg, fmt.Errorf("config_yaml %s must be a mapping", key)
			}
		case "system_template":
			if value.Kind != yaml.ScalarNode {
				return cfg, fmt.Errorf("config_yaml system_template must be a string")
			}
		}
	}

	metaNode, _ := mappingValueByKey(root, "meta")
	metaID, ok := mappingScalarByKey(metaNode, "id")
	if !ok || strings.TrimSpace(metaID) == "" {
		return cfg, fmt.Errorf("config_yaml meta.id is required")
	}

	metaName, ok := mappingScalarByKey(metaNode, "name")
	if !ok || strings.TrimSpace(metaName) == "" {
		return cfg, fmt.Errorf("config_yaml meta.name is required")
	}

	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		return cfg, fmt.Errorf("config_yaml decode failed: %w", err)
	}

	// Double-check after decode to avoid edge cases with non-string scalar values.
	if strings.TrimSpace(cfg.Meta.ID) == "" {
		return cfg, fmt.Errorf("config_yaml meta.id is required")
	}
	if strings.TrimSpace(cfg.Meta.Name) == "" {
		return cfg, fmt.Errorf("config_yaml meta.name is required")
	}

	return cfg, nil
}

func rootMappingNode(doc *yaml.Node) (*yaml.Node, error) {
	if doc == nil || doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("config_yaml must be a YAML document")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config_yaml root must be a mapping")
	}
	return root, nil
}

func mappingValueByKey(node *yaml.Node, key string) (*yaml.Node, bool) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1], true
		}
	}
	return nil, false
}

func mappingScalarByKey(node *yaml.Node, key string) (string, bool) {
	valueNode, ok := mappingValueByKey(node, key)
	if !ok || valueNode.Kind != yaml.ScalarNode {
		return "", false
	}
	return valueNode.Value, true
}
