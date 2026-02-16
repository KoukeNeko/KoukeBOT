import { isMap, isScalar, parseDocument } from 'yaml'

import type { GenerationConfig, PersonaCoreConfig } from '../../types'

export const REQUIRED_CORE_KEYS = ['meta', 'system_template', 'generation', 'variables'] as const

export const DEFAULT_PERSONA_CORE_CONFIG: PersonaCoreConfig = {
  meta: {
    id: '',
    name: '',
    description: '',
    version: '',
    author: '',
  },
  system_template: '',
  generation: {
    temperature: 0,
    max_tokens: 0,
    top_p: 0,
    frequency_penalty: 0,
    presence_penalty: 0,
  },
  variables: {},
}

export type ConfigYamlParseResult = {
  core: PersonaCoreConfig | null
  error: string | null
}

export function parseConfigYaml(configYaml: string): ConfigYamlParseResult {
  const doc = parseDocument(configYaml)
  if (doc.errors.length > 0) {
    return { core: null, error: formatDocumentErrors(doc.errors.map((err) => err.message)) }
  }

  const root = doc.contents
  if (!isMap(root)) {
    return { core: null, error: 'config.yaml root must be a mapping object.' }
  }

  for (const key of REQUIRED_CORE_KEYS) {
    if (!root.has(key)) {
      return { core: null, error: `config.yaml missing required key: ${key}` }
    }
  }

  const data = asRecord(doc.toJS())
  if (!data) {
    return { core: null, error: 'config.yaml root must be an object.' }
  }

  const meta = asRecord(data.meta)
  if (!meta) {
    return { core: null, error: 'config.yaml meta must be an object.' }
  }

  const metaID = asString(meta.id).trim()
  if (!metaID) {
    return { core: null, error: 'config.yaml meta.id is required.' }
  }

  const metaName = asString(meta.name).trim()
  if (!metaName) {
    return { core: null, error: 'config.yaml meta.name is required.' }
  }

  if (typeof data.system_template !== 'string') {
    return { core: null, error: 'config.yaml system_template must be a string.' }
  }

  const generation = asRecord(data.generation)
  if (!generation) {
    return { core: null, error: 'config.yaml generation must be an object.' }
  }

  const variables = asRecord(data.variables)
  if (!variables) {
    return { core: null, error: 'config.yaml variables must be an object.' }
  }

  const parsedGeneration: GenerationConfig = {
    temperature: asNumber(generation.temperature, 0),
    max_tokens: Math.trunc(asNumber(generation.max_tokens, 0)),
    top_p: asNumber(generation.top_p, 0),
    frequency_penalty: asNumber(generation.frequency_penalty, 0),
    presence_penalty: asNumber(generation.presence_penalty, 0),
  }

  return {
    core: {
      meta: {
        id: metaID,
        name: metaName,
        description: asString(meta.description),
        version: asString(meta.version),
        author: asString(meta.author),
      },
      system_template: data.system_template,
      generation: parsedGeneration,
      variables,
    },
    error: null,
  }
}

export function applyCoreConfigToYAML(configYaml: string, core: PersonaCoreConfig): string {
  const doc = parseDocument(configYaml)
  if (doc.errors.length > 0) {
    throw new Error(formatDocumentErrors(doc.errors.map((err) => err.message)))
  }

  const rootNode = doc.contents
  if (!isMap(rootNode)) {
    throw new Error('config.yaml root must be a mapping object.')
  }
  const root = rootNode as YamlMapNode

  const metaMap = ensureMapNode(root, 'meta')
  setScalarValue(metaMap, 'id', core.meta.id)
  setScalarValue(metaMap, 'name', core.meta.name)
  setScalarValue(metaMap, 'description', core.meta.description)
  setScalarValue(metaMap, 'version', core.meta.version)
  setScalarValue(metaMap, 'author', core.meta.author)

  setScalarValue(root, 'system_template', core.system_template)

  const generationMap = ensureMapNode(root, 'generation')
  setNumberValue(generationMap, 'temperature', core.generation.temperature)
  setNumberValue(generationMap, 'max_tokens', Math.trunc(core.generation.max_tokens))
  setNumberValue(generationMap, 'top_p', core.generation.top_p)
  setNumberValue(generationMap, 'frequency_penalty', core.generation.frequency_penalty)
  setNumberValue(generationMap, 'presence_penalty', core.generation.presence_penalty)

  root.set('variables', core.variables ?? {})

  return doc.toString()
}

type YamlMapNode = any

function ensureMapNode(mapNode: YamlMapNode, key: string): YamlMapNode {
  const existing = mapNode.get(key, true)
  if (isMap(existing)) {
    return existing
  }
  mapNode.set(key, {})
  const created = mapNode.get(key, true)
  if (!isMap(created)) {
    throw new Error(`config.yaml ${key} must be a mapping object.`)
  }
  return created
}

function setScalarValue(mapNode: YamlMapNode, key: string, value: string) {
  const existing = mapNode.get(key, true)
  if (isScalar(existing)) {
    existing.value = value
    return
  }
  mapNode.set(key, value)
}

function setNumberValue(mapNode: YamlMapNode, key: string, value: number) {
  const existing = mapNode.get(key, true)
  if (isScalar(existing)) {
    existing.value = value
    return
  }
  mapNode.set(key, value)
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return null
  }
  return value as Record<string, unknown>
}

function asString(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function asNumber(value: unknown, fallback: number): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : fallback
}

function formatDocumentErrors(messages: string[]): string {
  const cleaned = messages.map((msg) => msg.trim()).filter(Boolean)
  if (cleaned.length === 0) {
    return 'Invalid YAML.'
  }
  return cleaned.join('\n')
}
