import type { ScheduledTask } from '../types'
import { BASE, fetchJSON } from './client'

export function listScheduledTasks(options?: {
  channelId?: string
  creatorUserId?: string
  includeInactive?: boolean
}): Promise<ScheduledTask[]> {
  const params = new URLSearchParams()
  if (options?.channelId) params.set('channel_id', options.channelId)
  if (options?.creatorUserId) params.set('creator_user_id', options.creatorUserId)
  if (typeof options?.includeInactive === 'boolean') {
    params.set('include_inactive', String(options.includeInactive))
  }
  const query = params.toString()
  const url = query ? `${BASE}/schedules?${query}` : `${BASE}/schedules`
  return fetchJSON(url)
}
