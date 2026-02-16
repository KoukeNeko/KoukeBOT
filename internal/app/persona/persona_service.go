package persona

import (
	"fmt"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/persona"
)

// PersonaService manages persona lifecycle: loading, listing, hot-reload.
type PersonaService struct {
	baseDir  string
	personas map[string]*domain.PersonaModule
}

func NewPersonaService(baseDir string) (*PersonaService, error) {
	personas, err := persona.LoadAllPersonas(baseDir)
	if err != nil {
		return nil, fmt.Errorf("load personas from %q: %w", baseDir, err)
	}

	return &PersonaService{
		baseDir:  baseDir,
		personas: personas,
	}, nil
}

// GetAll returns all loaded personas.
func (s *PersonaService) GetAll() map[string]*domain.PersonaModule {
	return s.personas
}

// Get returns a single persona by ID.
func (s *PersonaService) Get(id string) (*domain.PersonaModule, bool) {
	p, ok := s.personas[id]
	return p, ok
}

// GetConfigRaw returns the raw config.yaml content for the persona.
func (s *PersonaService) GetConfigRaw(id string) (string, error) {
	p, ok := s.personas[id]
	if !ok {
		return "", fmt.Errorf("persona not found: %q", id)
	}

	raw, err := persona.LoadPersonaConfigRaw(s.baseDir, p.DirName)
	if err != nil {
		return "", fmt.Errorf("load raw config for persona %q: %w", id, err)
	}
	return raw, nil
}

// Reload re-reads all persona folders from disk.
func (s *PersonaService) Reload() error {
	personas, err := persona.LoadAllPersonas(s.baseDir)
	if err != nil {
		return fmt.Errorf("reload personas: %w", err)
	}
	s.personas = personas
	return nil
}

// Update persists a persona module to disk and refreshes in-memory state.
func (s *PersonaService) Update(id string, module *domain.PersonaModule) error {
	existing, ok := s.personas[id]
	if !ok {
		return fmt.Errorf("persona not found: %q", id)
	}

	// Persist into the original directory, not meta.id.
	module.DirName = existing.DirName
	if module.Config.Meta.ID == "" {
		module.Config.Meta.ID = id
	}

	if err := persona.SavePersona(s.baseDir, module.DirName, module); err != nil {
		return fmt.Errorf("save persona %q: %w", id, err)
	}

	if module.Config.Meta.ID != id {
		delete(s.personas, id)
	}
	s.personas[module.Config.Meta.ID] = module
	return nil
}

// UpdateWithRawConfig persists persona files while writing config.yaml as raw text.
func (s *PersonaService) UpdateWithRawConfig(id, rawConfig string, module *domain.PersonaModule) error {
	existing, ok := s.personas[id]
	if !ok {
		return fmt.Errorf("persona not found: %q", id)
	}

	module.DirName = existing.DirName
	if module.Config.Meta.ID == "" {
		module.Config.Meta.ID = id
	}

	if err := persona.SavePersonaWithRawConfig(s.baseDir, module.DirName, rawConfig, module); err != nil {
		return fmt.Errorf("save persona %q with raw config: %w", id, err)
	}

	if module.Config.Meta.ID != id {
		delete(s.personas, id)
	}
	s.personas[module.Config.Meta.ID] = module
	return nil
}
