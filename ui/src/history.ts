import { PluginUIError, type BotAccount, type PluginUIClient } from '@rayleabot/plugin-ui'
import type { Subject } from './model'

export type HistoryState = 'pending' | 'outside' | 'present' | 'unknown'
export async function subjectState(client: Pick<PluginUIClient, 'apiRequest'>, owner?: Subject): Promise<HistoryState> {
  if (!owner) return 'unknown'
  if (!owner.bot_id) return 'pending'
  try {
    await client.apiRequest('GET', `/api/bot-accounts/${encodeURIComponent(owner.source_protocol)}/${encodeURIComponent(owner.bot_id)}`)
    return 'present'
  } catch (cause) {
    if (cause instanceof PluginUIError && cause.status === 404) return 'outside'
    throw cause
  }
}

export async function assignmentBots(client: Pick<PluginUIClient, 'apiRequest'>): Promise<BotAccount[]> {
  const items: BotAccount[] = []
  let cursor: string | undefined
  do {
    const query = new URLSearchParams({ source_protocol: 'onebot11', limit: '100' })
    if (cursor !== undefined) query.set('cursor', cursor)
    const page = await client.apiRequest<{ items: BotAccount[]; next_cursor?: string }>('GET', `/api/bot-accounts?${query}`)
    items.push(...page.items); cursor = page.next_cursor
  } while (cursor !== undefined)
  return items
}
