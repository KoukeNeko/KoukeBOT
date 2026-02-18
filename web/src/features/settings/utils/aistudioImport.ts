import type { AIStudioSessionInput } from '../../../types'

const makerSuiteRPCPrefix =
  '/$rpc/google.internal.alkali.applications.makersuite.v1.MakerSuiteService/'

type HARHeader = {
  name?: string
  value?: string
}

type HARRequest = {
  method?: string
  url?: string
  headers?: HARHeader[]
}

type HAREntry = {
  startedDateTime?: string
  request?: HARRequest
}

type HARLog = {
  entries?: HAREntry[]
}

type HARShape = {
  log?: HARLog
}

export type AIStudioImportSourceType = 'har' | 'session_json' | 'curl'

export interface AIStudioImportResult {
  sourceType: AIStudioImportSourceType
  session: AIStudioSessionInput
  warnings: string[]
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return null
  }
  return value as Record<string, unknown>
}

function normalizeString(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function isHARShape(value: unknown): value is HARShape {
  const record = asRecord(value)
  const log = record ? asRecord(record.log) : null
  return !!log && Array.isArray(log.entries)
}

function hasKnownSessionField(value: Record<string, unknown>): boolean {
  const knownFields = [
    'endpoint',
    'api_key',
    'x_browser_validation',
    'x_client_data',
    'x_goog_authuser',
    'x_goog_ext_519733851_bin',
    'cookie',
    'authorization',
    'user_agent',
    'accept_language',
    'origin',
    'referer',
    'x_browser_channel',
    'x_browser_copyright',
    'x_browser_year',
  ]
  return knownFields.some((key) => key in value)
}

function normalizeSessionInput(raw: Record<string, unknown>): AIStudioSessionInput {
  const source = asRecord(raw.session) && !hasKnownSessionField(raw) ? asRecord(raw.session)! : raw
  return {
    endpoint: normalizeString(source.endpoint),
    api_key: normalizeString(source.api_key),
    x_browser_validation: normalizeString(source.x_browser_validation),
    x_client_data: normalizeString(source.x_client_data),
    x_goog_authuser: normalizeString(source.x_goog_authuser) || '0',
    x_goog_ext_519733851_bin: normalizeString(source.x_goog_ext_519733851_bin),
    cookie: normalizeString(source.cookie),
    authorization: normalizeString(source.authorization),
    user_agent: normalizeString(source.user_agent),
    accept_language: normalizeString(source.accept_language),
    origin: normalizeString(source.origin),
    referer: normalizeString(source.referer),
    x_browser_channel: normalizeString(source.x_browser_channel),
    x_browser_copyright: normalizeString(source.x_browser_copyright),
    x_browser_year: normalizeString(source.x_browser_year),
  }
}

function headersToMap(headers: HARHeader[] | undefined): Record<string, string> {
  const out: Record<string, string> = {}
  for (const header of headers ?? []) {
    const name = normalizeString(header.name).toLowerCase()
    if (!name) continue
    out[name] = normalizeString(header.value)
  }
  return out
}

function parseEntryTimestamp(entry: HAREntry, fallback: number): number {
  const millis = Date.parse(normalizeString(entry.startedDateTime))
  if (Number.isFinite(millis)) return millis
  return fallback
}

function pickLatestHARRequest(entries: HAREntry[]): HAREntry | null {
  if (entries.length === 0) return null
  let latest: HAREntry | null = null
  let latestTime = Number.NEGATIVE_INFINITY
  for (let i = 0; i < entries.length; i++) {
    const entry = entries[i]
    const t = parseEntryTimestamp(entry, i)
    if (!latest || t >= latestTime) {
      latest = entry
      latestTime = t
    }
  }
  return latest
}

function buildWarnings(session: AIStudioSessionInput): string[] {
  const warnings: string[] = []
  if (!session.cookie && !session.authorization) {
    warnings.push('缺少 cookie/authorization，verify 很可能失敗。')
  }
  if (!session.x_browser_validation) {
    warnings.push('缺少 x-browser-validation。')
  }
  if (!session.x_client_data) {
    warnings.push('缺少 x-client-data。')
  }
  if (!session.api_key) {
    warnings.push('缺少 x-goog-api-key，後端會套用預設值。')
  }
  if (!session.x_goog_ext_519733851_bin) {
    warnings.push('缺少 x-goog-ext-519733851-bin，後端會套用預設值。')
  }
  if (session.endpoint) {
    try {
      const host = new URL(session.endpoint).hostname
      if (host !== 'alkalimakersuite-pa.clients6.google.com') {
        warnings.push(`endpoint host 非預期：${host}`)
      }
    } catch {
      warnings.push('endpoint 格式無法解析。')
    }
  }
  return warnings
}

function parseHARImport(raw: HARShape): AIStudioImportResult {
  const entries = raw.log?.entries ?? []
  const rpcEntries = entries.filter((entry) => {
    const req = entry.request
    const method = normalizeString(req?.method).toUpperCase()
    const url = normalizeString(req?.url)
    return method === 'POST' && url.includes(makerSuiteRPCPrefix)
  })
  const generateEntries = rpcEntries.filter((entry) =>
    normalizeString(entry.request?.url).includes(`${makerSuiteRPCPrefix}GenerateContent`),
  )
  const selected = pickLatestHARRequest(generateEntries.length > 0 ? generateEntries : rpcEntries)
  if (!selected?.request) {
    throw new Error('HAR 內找不到 MakerSuiteService RPC 請求。')
  }

  const request = selected.request
  const urlText = normalizeString(request.url)
  let endpoint = ''
  if (urlText) {
    try {
      endpoint = new URL(urlText).origin
    } catch {
      endpoint = ''
    }
  }
  const headers = headersToMap(request.headers)
  const session: AIStudioSessionInput = {
    endpoint,
    api_key: headers['x-goog-api-key'] ?? '',
    x_browser_validation: headers['x-browser-validation'] ?? '',
    x_client_data: headers['x-client-data'] ?? '',
    x_goog_authuser: headers['x-goog-authuser'] || '0',
    x_goog_ext_519733851_bin: headers['x-goog-ext-519733851-bin'] ?? '',
    cookie: headers.cookie ?? '',
    authorization: headers.authorization ?? '',
    user_agent: headers['user-agent'] ?? '',
    accept_language: headers['accept-language'] ?? '',
    origin: headers.origin ?? '',
    referer: headers.referer ?? '',
    x_browser_channel: headers['x-browser-channel'] ?? '',
    x_browser_copyright: headers['x-browser-copyright'] ?? '',
    x_browser_year: headers['x-browser-year'] ?? '',
  }

  return {
    sourceType: 'har',
    session,
    warnings: buildWarnings(session),
  }
}

function parseSessionJSONImport(raw: unknown): AIStudioImportResult {
  const record = asRecord(raw)
  if (!record) {
    throw new Error('Session JSON 格式無效。')
  }

  const session = normalizeSessionInput(record)
  if (!hasKnownSessionField(record) && !(asRecord(record.session) && hasKnownSessionField(asRecord(record.session)!))) {
    throw new Error('JSON 不是 HAR，也不是可識別的 AI Studio session 格式。')
  }

  return {
    sourceType: 'session_json',
    session,
    warnings: buildWarnings(session),
  }
}

function isCurlLikeText(text: string): boolean {
  const trimmed = stripCodeFence(text).trimStart()
  if (!trimmed) return false
  if (trimmed.startsWith('curl ') || trimmed === 'curl') return true
  if (trimmed.startsWith('$ curl')) return true
  return false
}

function stripCodeFence(text: string): string {
  const trimmed = text.trim()
  if (!trimmed.startsWith('```') || !trimmed.endsWith('```')) {
    return text
  }
  const lines = trimmed.split(/\r?\n/)
  if (lines.length < 2) return text
  return lines.slice(1, -1).join('\n')
}

function decodeANSICLikeEscape(ch: string): string {
  switch (ch) {
    case 'n':
      return '\n'
    case 'r':
      return '\r'
    case 't':
      return '\t'
    case '\\':
      return '\\'
    case "'":
      return "'"
    case '"':
      return '"'
    default:
      return ch
  }
}

function tokenizeShellLike(input: string): string[] {
  const tokens: string[] = []
  let current = ''
  let quote: 'single' | 'double' | 'ansi' | null = null

  for (let i = 0; i < input.length; i++) {
    const ch = input[i]

    if (quote === null) {
      if (/\s/.test(ch)) {
        if (current) {
          tokens.push(current)
          current = ''
        }
        continue
      }
      if (ch === "'") {
        quote = 'single'
        continue
      }
      if (ch === '"') {
        quote = 'double'
        continue
      }
      if (ch === '$' && input[i + 1] === "'") {
        quote = 'ansi'
        i += 1
        continue
      }
      if (ch === '\\' && i + 1 < input.length) {
        i += 1
        current += input[i]
        continue
      }
      current += ch
      continue
    }

    if (quote === 'single') {
      if (ch === "'") {
        quote = null
        continue
      }
      current += ch
      continue
    }

    if (quote === 'double') {
      if (ch === '"') {
        quote = null
        continue
      }
      if (ch === '\\' && i + 1 < input.length) {
        i += 1
        current += input[i]
        continue
      }
      current += ch
      continue
    }

    if (ch === "'") {
      quote = null
      continue
    }
    if (ch === '\\' && i + 1 < input.length) {
      i += 1
      current += decodeANSICLikeEscape(input[i])
      continue
    }
    current += ch
  }

  if (quote !== null) {
    throw new Error('cURL 內容引號未正確結束。')
  }
  if (current) {
    tokens.push(current)
  }
  return tokens
}

function normalizeCurlCommand(rawText: string): string {
  const unwrapped = stripCodeFence(rawText).trim()
  const lines = unwrapped
    .split(/\r?\n/)
    .map((line) => line.replace(/^\s*\$\s*/, ''))
  return lines.join('\n').replace(/\\\s*(\r?\n)/g, ' ').trim()
}

function parseHeaderValueToMap(headerValue: string, out: Record<string, string>): void {
  const index = headerValue.indexOf(':')
  if (index <= 0) return
  const name = headerValue.slice(0, index).trim().toLowerCase()
  const value = headerValue.slice(index + 1).trim()
  if (!name) return
  out[name] = value
}

function chooseRPCURL(candidates: string[]): string {
  if (candidates.length === 0) return ''
  const generate = candidates.find((value) =>
    value.includes(`${makerSuiteRPCPrefix}GenerateContent`),
  )
  if (generate) return generate
  const makerSuite = candidates.find((value) => value.includes(makerSuiteRPCPrefix))
  if (makerSuite) return makerSuite
  return candidates[0]
}

function parseCurlImport(rawText: string): AIStudioImportResult {
  const normalized = normalizeCurlCommand(rawText)
  const tokens = tokenizeShellLike(normalized)
  if (tokens.length === 0) {
    throw new Error('cURL 內容為空。')
  }
  if (tokens[0] !== 'curl') {
    throw new Error('檔案不是有效 cURL 指令。')
  }

  const headers: Record<string, string> = {}
  const urlCandidates: string[] = []
  let cookieFromOption = ''
  let userAgentFromOption = ''
  let refererFromOption = ''
  let originFromOption = ''

  for (let i = 1; i < tokens.length; i++) {
    const token = tokens[i]
    if (token === '-H' || token === '--header') {
      const headerValue = tokens[i + 1] ?? ''
      i += 1
      parseHeaderValueToMap(headerValue, headers)
      continue
    }
    if (token.startsWith('-H') && token.length > 2) {
      parseHeaderValueToMap(token.slice(2), headers)
      continue
    }
    if (token === '--url') {
      const urlValue = tokens[i + 1] ?? ''
      i += 1
      if (urlValue.startsWith('http://') || urlValue.startsWith('https://')) {
        urlCandidates.push(urlValue)
      }
      continue
    }
    if (token === '-b' || token === '--cookie') {
      cookieFromOption = tokens[i + 1] ?? ''
      i += 1
      continue
    }
    if (token === '-A' || token === '--user-agent') {
      userAgentFromOption = tokens[i + 1] ?? ''
      i += 1
      continue
    }
    if (token === '-e' || token === '--referer') {
      refererFromOption = tokens[i + 1] ?? ''
      i += 1
      continue
    }
    if (token === '--origin') {
      originFromOption = tokens[i + 1] ?? ''
      i += 1
      continue
    }
    if (token.startsWith('http://') || token.startsWith('https://')) {
      urlCandidates.push(token)
    }
  }

  const rpcURL = chooseRPCURL(urlCandidates)
  if (!rpcURL) {
    throw new Error('cURL 內找不到 RPC URL。')
  }

  let endpoint = ''
  try {
    endpoint = new URL(rpcURL).origin
  } catch {
    endpoint = ''
  }

  const cookie = headers.cookie || cookieFromOption
  const session: AIStudioSessionInput = {
    endpoint,
    api_key: headers['x-goog-api-key'] ?? '',
    x_browser_validation: headers['x-browser-validation'] ?? '',
    x_client_data: headers['x-client-data'] ?? '',
    x_goog_authuser: headers['x-goog-authuser'] || '0',
    x_goog_ext_519733851_bin: headers['x-goog-ext-519733851-bin'] ?? '',
    cookie,
    authorization: headers.authorization ?? '',
    user_agent: headers['user-agent'] || userAgentFromOption,
    accept_language: headers['accept-language'] ?? '',
    origin: headers.origin || originFromOption,
    referer: headers.referer || refererFromOption,
    x_browser_channel: headers['x-browser-channel'] ?? '',
    x_browser_copyright: headers['x-browser-copyright'] ?? '',
    x_browser_year: headers['x-browser-year'] ?? '',
  }

  return {
    sourceType: 'curl',
    session,
    warnings: buildWarnings(session),
  }
}

export function parseAIStudioImport(rawText: string): AIStudioImportResult {
  const text = rawText.trim()
  if (!text) {
    throw new Error('上傳內容為空。')
  }

  if (isCurlLikeText(text)) {
    return parseCurlImport(text)
  }

  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    throw new Error('檔案不是有效 JSON 或 cURL。')
  }

  if (isHARShape(parsed)) {
    return parseHARImport(parsed)
  }
  return parseSessionJSONImport(parsed)
}
