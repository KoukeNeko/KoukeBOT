package app

import personapkg "github.com/KoukeNeko/KoukeBOT/internal/app/persona"

// Persona facade keeps existing app-level API stable while persona internals
// live in a dedicated package.
type PersonaService = personapkg.PersonaService

func NewPersonaService(baseDir string) (*PersonaService, error) {
	return personapkg.NewPersonaService(baseDir)
}
