# AI Studio RPC Runbook

## Purpose
Describe how to configure and operate AI Studio RPC in KoukeBOT, including:
- OAuth exhaustion fallback mode
- direct provider mode (force switch for testing)
- session refresh workflow when credentials expire

## Scope
- Text-only path for AI Studio RPC.
- Multimodal requests do not use AI Studio RPC fallback.
- This integration is reverse-engineered and may break when Google changes AI Studio internals.

## Prerequisites
- You can open [https://aistudio.google.com](https://aistudio.google.com) in a browser and send a prompt successfully.
- You can export either:
  - HAR from browser DevTools, or
  - copied cURL from Network tab, or
  - `aistudio_session.json` payload.

## Setup Flow (Recommended)
1. Open `Settings -> LLM Provider -> AI Studio Fallback`.
2. Upload or paste HAR / session JSON / cURL.
3. Click `Parse Pasted Text` (if using textarea input).
4. Confirm parsed summary:
   - `endpoint`
   - `x_browser_validation`
   - `x_client_data`
   - `x_goog_authuser`
   - at least one of `cookie` or `authorization` should be present.
5. Click `Upload & Verify & Apply`.
6. Verify success message and `已設定 AI Studio session`.

Important:
- Verify is strict. If verify fails, session is not saved.
- Secrets are masked in UI and logs.

## Two Runtime Modes

### A) Fallback mode (default)
- Keep provider as OAuth pool (`google-antigravity` or `google-gemini-cli`).
- AI Studio is used only when OAuth accounts are quota exhausted.

### B) Direct provider mode (force for testing)
1. In `Settings -> LLM Provider -> Gemini OAuth` section, set `Provider` to `AI Studio RPC (Direct / Test)`.
2. Pick a Gemini model (`aistudio-rpc/...`).
3. Save with `Use Selected Provider`.

Behavior:
- Requests go directly through AI Studio RPC provider.
- Session `PUT/DELETE` will hot-update provider runtime without restart.

## Session Expiration and Refresh
- AI Studio cookies / SAPISID-derived auth can expire.
- KoukeBOT tries token refresh using AI Studio RPC access-token path when possible.
- If verify or chat starts returning auth errors again, re-import fresh HAR/cURL/session.

Recommended refresh workflow:
1. Reproduce one successful request in browser AI Studio.
2. Re-export HAR or copy latest cURL from `MakerSuiteService/GenerateContent`.
3. Re-parse and `Upload & Verify & Apply`.

## Troubleshooting

### 401 `CREDENTIALS_MISSING` / `API keys are not supported`
Cause:
- Missing principal credential for this RPC call.
- Typically `cookie` and/or `authorization` is missing or expired.

Fix:
1. Re-capture cURL/HAR from a successful AI Studio web request.
2. Ensure imported data contains valid `cookie` or `authorization`.
3. Re-run verify.

### 400 `Request contains an invalid argument`
Cause:
- Usually invalid model or payload mismatch.

Fix:
1. Use Gemini model IDs only (for example `gemini-3-pro-preview` or `gemini-2.5-pro`).
2. Avoid non-Gemini model values in AI Studio provider mode.
3. Re-verify with `Verify Current Session`.

### Verify fails but parse succeeds
Cause:
- Parsed headers are syntactically valid but no longer authorized.

Fix:
- Re-capture latest browser credentials and apply again.

## Security Notes
- Do not share raw HAR/cURL/session files publicly.
- Treat `cookie`, `authorization`, and any browser validation headers as sensitive credentials.
- Rotate session immediately if leaked.
