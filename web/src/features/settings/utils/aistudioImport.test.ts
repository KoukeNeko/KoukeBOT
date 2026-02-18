import { describe, expect, it } from 'vitest'

import { parseAIStudioImport } from './aistudioImport'

describe('parseAIStudioImport', () => {
  it('parses HAR and extracts latest GenerateContent session fields', () => {
    const har = {
      log: {
        entries: [
          {
            startedDateTime: '2026-02-18T09:00:00Z',
            request: {
              method: 'POST',
              url: 'https://alkalimakersuite-pa.clients6.google.com/$rpc/google.internal.alkali.applications.makersuite.v1.MakerSuiteService/GenerateContent',
              headers: [
                { name: 'x-goog-api-key', value: 'k1' },
                { name: 'x-browser-validation', value: 'b1' },
                { name: 'x-client-data', value: 'c1' },
                { name: 'x-goog-authuser', value: '1' },
                { name: 'x-goog-ext-519733851-bin', value: 'ext1' },
                { name: 'cookie', value: 'SID=abc' },
                { name: 'user-agent', value: 'ua1' },
              ],
            },
          },
          {
            startedDateTime: '2026-02-18T10:00:00Z',
            request: {
              method: 'POST',
              url: 'https://alkalimakersuite-pa.clients6.google.com/$rpc/google.internal.alkali.applications.makersuite.v1.MakerSuiteService/GenerateContent',
              headers: [
                { name: 'x-goog-api-key', value: 'k2' },
                { name: 'x-browser-validation', value: 'b2' },
                { name: 'x-client-data', value: 'c2' },
                { name: 'x-goog-authuser', value: '0' },
                { name: 'x-goog-ext-519733851-bin', value: 'ext2' },
                { name: 'authorization', value: 'Bearer xyz' },
              ],
            },
          },
        ],
      },
    }

    const result = parseAIStudioImport(JSON.stringify(har))
    expect(result.sourceType).toBe('har')
    expect(result.session.endpoint).toBe('https://alkalimakersuite-pa.clients6.google.com')
    expect(result.session.api_key).toBe('k2')
    expect(result.session.x_browser_validation).toBe('b2')
    expect(result.session.x_client_data).toBe('c2')
    expect(result.session.x_goog_authuser).toBe('0')
    expect(result.session.x_goog_ext_519733851_bin).toBe('ext2')
    expect(result.model).toBe('')
    expect(result.warnings.length).toBe(0)
  })

  it('parses session JSON payload', () => {
    const payload = {
      endpoint: 'https://alkalimakersuite-pa.clients6.google.com',
      api_key: 'api-key',
      x_browser_validation: 'validation',
      x_client_data: 'client-data',
      x_goog_authuser: '0',
      cookie: 'SID=abc',
      user_agent: 'Mozilla/test',
    }

    const result = parseAIStudioImport(JSON.stringify(payload))
    expect(result.sourceType).toBe('session_json')
    expect(result.session.api_key).toBe('api-key')
    expect(result.session.x_browser_validation).toBe('validation')
    expect(result.session.cookie).toContain('SID=')
    expect(result.model).toBe('')
  })

  it('reports warnings for missing cookie/authorization', () => {
    const payload = {
      endpoint: 'https://alkalimakersuite-pa.clients6.google.com',
      api_key: 'api-key',
      x_browser_validation: 'validation',
      x_client_data: 'client-data',
      x_goog_authuser: '0',
    }

    const result = parseAIStudioImport(JSON.stringify(payload))
    expect(result.warnings.some((item) => item.includes('cookie/authorization'))).toBe(true)
  })

  it('parses cURL command and maps headers/cookie', () => {
    const curl = `
curl 'https://alkalimakersuite-pa.clients6.google.com/$rpc/google.internal.alkali.applications.makersuite.v1.MakerSuiteService/GenerateContent' \\
  -H 'accept-language: ja,en-US;q=0.9,en;q=0.8' \\
  -H 'authorization: SAPISIDHASH testhash' \\
  -H 'x-browser-validation: validation-token' \\
  -H 'x-client-data: client-data-token' \\
  -H 'x-goog-api-key: key-123' \\
  -H 'x-goog-authuser: 0' \\
  -H 'x-goog-ext-519733851-bin: ext-token' \\
  -H 'x-browser-channel: beta' \\
  -H 'user-agent: Mozilla/test' \\
  -b 'SID=abc; HSID=def' \\
  --data-raw $'["models/gemini-3-pro-preview",[]]'
`

    const result = parseAIStudioImport(curl)
    expect(result.sourceType).toBe('curl')
    expect(result.session.endpoint).toBe('https://alkalimakersuite-pa.clients6.google.com')
    expect(result.session.api_key).toBe('key-123')
    expect(result.session.x_browser_validation).toBe('validation-token')
    expect(result.session.x_client_data).toBe('client-data-token')
    expect(result.session.x_goog_authuser).toBe('0')
    expect(result.session.x_goog_ext_519733851_bin).toBe('ext-token')
    expect(result.session.cookie).toContain('SID=abc')
    expect(result.session.authorization).toContain('SAPISIDHASH')
    expect(result.model).toBe('gemini-3-pro-preview')
    expect(result.warnings.length).toBe(0)
  })

  it('parses cURL from fenced code block', () => {
    const curl = `\`\`\`bash
curl 'https://alkalimakersuite-pa.clients6.google.com/$rpc/google.internal.alkali.applications.makersuite.v1.MakerSuiteService/CountTokens' -H 'x-browser-validation: b' -H 'x-client-data: c'
\`\`\``

    const result = parseAIStudioImport(curl)
    expect(result.sourceType).toBe('curl')
    expect(result.session.endpoint).toBe('https://alkalimakersuite-pa.clients6.google.com')
    expect(result.model).toBe('')
    expect(result.warnings.some((item) => item.includes('cookie/authorization'))).toBe(true)
  })

  it('throws for malformed JSON', () => {
    expect(() => parseAIStudioImport('{oops')).toThrow('檔案不是有效 JSON 或 cURL。')
  })
})
