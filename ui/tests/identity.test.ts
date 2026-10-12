import { expect, it, vi } from 'vitest'
import { personOf, resolveNicknames } from '../src/identity'

const bot = { source_protocol: 'onebot11', bot_id: '10001', state: 'online', adapter_id: 'adapter A' }

it('resolves owners through the bot connection in batches and keeps numbers without a nickname', async () => {
  const ids = Array.from({ length: 101 }, (_, index) => String(20000000 + index))
  const apiRequest = vi.fn(async (_method: string, _path: string, body: { items: { user_id: string }[] }) => ({
    items: body.items.map(item => ({ user_id: item.user_id, nickname: item.user_id === ids[1] ? item.user_id : `示例${item.user_id}` })),
  }))
  const names = await resolveNicknames({ refreshBot: async () => bot, client: { apiRequest } } as never, ids)
  expect(apiRequest.mock.calls.map(([method, path, body]) => [method, path, body.items.length])).toEqual([
    ['POST', '/api/adapters/adapter%20A/onebot11/identities/resolve', 100],
    ['POST', '/api/adapters/adapter%20A/onebot11/identities/resolve', 1],
  ])
  expect(apiRequest.mock.calls[0][2].items[0]).toEqual({ target_type: 'private', target_id: ids[0], user_id: ids[0] })
  expect(names?.get(ids[0])).toBe(`示例${ids[0]}`)
  expect(names?.has(ids[1])).toBe(false)
  expect(personOf({ source_protocol: 'onebot11', bot_id: '10001', actor_id: ids[1] }, names!).name).toBe(`QQ ${ids[1]}`)
})

it('does not ask an offline bot and gives openid users no QQ avatar', async () => {
  const apiRequest = vi.fn()
  expect(await resolveNicknames({ refreshBot: async () => ({ ...bot, state: 'offline' }), client: { apiRequest } } as never, ['20000000'])).toBeNull()
  expect(apiRequest).not.toHaveBeenCalled()
  const person = personOf({ source_protocol: 'qqofficial', bot_id: 'app', actor_id: 'A1B2C3' }, new Map())
  expect(person.avatar).toBeUndefined()
  expect(person.idLabel).toBe('ID')
})
