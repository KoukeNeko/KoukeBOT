export interface AIStudioSessionInput {
  endpoint?: string
  api_key?: string
  x_browser_validation: string
  x_client_data: string
  x_goog_authuser?: string
  x_goog_ext_519733851_bin?: string
  cookie?: string
  authorization?: string
  user_agent?: string
  accept_language?: string
  origin?: string
  referer?: string
  x_browser_channel?: string
  x_browser_copyright?: string
  x_browser_year?: string
}

export interface AIStudioSessionMasked {
  endpoint?: string
  api_key?: string
  x_browser_validation?: string
  x_client_data?: string
  x_goog_authuser?: string
  x_goog_ext_519733851_bin?: string
  cookie?: string
  authorization?: string
  user_agent?: string
  accept_language?: string
  origin?: string
  referer?: string
  x_browser_channel?: string
  x_browser_copyright?: string
  x_browser_year?: string
}

export interface AIStudioSessionStatus {
  configured: boolean
  session?: AIStudioSessionMasked
}

export interface AIStudioVerifyResponse {
  verified: boolean
  model: string
  error?: string
}
