import { describe, expect, it } from 'vitest'

import {
  applyCoreConfigToYAML,
  parseConfigYaml,
  type ConfigYamlParseResult,
} from './configYaml'

const sampleConfigYAML = `meta:
  id: "persona-a"
  name: "Persona A"
  description: "desc"
  version: "1.0.0"
  author: "tester"
system_template: |
  hello
generation:
  temperature: 0.8
  max_tokens: 256
  top_p: 0.95
  frequency_penalty: 0.1
  presence_penalty: 0.1
variables:
  foo: "bar"
custom_root:
  keep: true
`

function expectValid(result: ConfigYamlParseResult) {
  expect(result.error).toBeNull()
  expect(result.core).not.toBeNull()
}

describe('persona config yaml helpers', () => {
  it('parses core config and validates required keys', () => {
    const parsed = parseConfigYaml(sampleConfigYAML)
    expectValid(parsed)
    expect(parsed.core?.meta.id).toBe('persona-a')
    expect(parsed.core?.system_template.trim()).toBe('hello')
  })

  it('updates core fields while preserving custom root keys', () => {
    const parsed = parseConfigYaml(sampleConfigYAML)
    expectValid(parsed)
    const core = parsed.core!

    const nextYAML = applyCoreConfigToYAML(sampleConfigYAML, {
      ...core,
      meta: {
        ...core.meta,
        name: 'Persona Updated',
      },
      generation: {
        ...core.generation,
        max_tokens: 1024,
      },
    })

    expect(nextYAML).toContain('custom_root:')
    expect(nextYAML).toContain('custom_root:')

    const reparsed = parseConfigYaml(nextYAML)
    expectValid(reparsed)
    expect(reparsed.core?.meta.name).toBe('Persona Updated')
    expect(reparsed.core?.generation.max_tokens).toBe(1024)
  })

  it('returns parse errors for invalid yaml or missing required key', () => {
    const invalid = parseConfigYaml('meta: [')
    expect(invalid.error).toBeTruthy()

    const missingVariables = parseConfigYaml(`meta:
  id: "persona-a"
  name: "Persona A"
system_template: "x"
generation: {}
`)
    expect(missingVariables.error).toContain('missing required key: variables')
  })
})
