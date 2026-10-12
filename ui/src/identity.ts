import type { usePluginHost } from '@rayleabot/plugin-ui'
import type { Subject } from './model'

type IdentityHost = Pick<ReturnType<typeof usePluginHost>, 'client' | 'refreshBot'>

export interface Person { name: string; id: string; idLabel: string; initial: string; avatar?: string; seed: string }

const qqID = /^[1-9][0-9]{4,11}$/

// Only OneBot11 users are QQ numbers; QQ official bots address users by openid,
// which has no public avatar or nickname.
export function isQQ(owner: Subject): boolean {
  return owner.source_protocol === 'onebot11' && qqID.test(owner.actor_id)
}

// QQ avatars load straight from the QQ avatar host; the plugin page CSP allows
// HTTPS images. A failed image falls back to the initial.
export function qqAvatar(id: string): string {
  return `https://q1.qlogo.cn/g?b=qq&nk=${encodeURIComponent(id)}&s=100`
}

// A long openid leads with a short form; the card shows the full value beside it.
export function personOf(owner: Subject, nicknames: ReadonlyMap<string, string>): Person {
  const id = owner.actor_id
  if (!isQQ(owner)) {
    const name = id.length > 12 ? `${id.slice(0, 4)}…${id.slice(-4)}` : `ID ${id}`
    return { name, id, idLabel: 'ID', initial: id.slice(0, 2).toUpperCase(), seed: id }
  }
  const nickname = nicknames.get(id)
  return { name: nickname || `QQ ${id}`, id, idLabel: 'QQ', initial: nickname ? [...nickname.trim()][0] ?? id.slice(-2) : id.slice(-2), avatar: qqAvatar(id), seed: id }
}

// resolveNicknames asks the bot's current OneBot11 connection for the users'
// profiles. It returns null when the bot cannot be asked right now.
export async function resolveNicknames(host: IdentityHost, ids: string[]): Promise<Map<string, string> | null> {
  const bot = await host.refreshBot()
  if (bot.source_protocol !== 'onebot11' || bot.state !== 'online' || !bot.adapter_id) return null
  const path = `/api/adapters/${encodeURIComponent(bot.adapter_id)}/onebot11/identities/resolve`
  const names = new Map<string, string>()
  for (let start = 0; start < ids.length; start += 100) {
    const items = ids.slice(start, start + 100).map(id => ({ target_type: 'private', target_id: id, user_id: id }))
    const response = await host.client.apiRequest<{ items: { user_id: string; nickname: string }[] }>('POST', path, { items })
    for (const item of response.items) {
      // The host falls back to the number itself when the profile has no nickname.
      if (item.nickname && item.nickname !== item.user_id) names.set(item.user_id, item.nickname)
    }
  }
  return names
}
