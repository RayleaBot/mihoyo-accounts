import { describe, expect, it, vi } from 'vitest'
import { PluginUIError } from '@rayleabot/plugin-ui'
import { assignmentBots, subjectState } from '../src/history'

describe('historical ownership', () => {
  it('uses individual directory lookup and treats only 404 as removed', async () => {
    const apiRequest = vi.fn().mockResolvedValue({ state: 'no_connection' })
    const owner = { source_protocol: 'qqofficial', bot_id: 'id:with/slash', actor_id: 'user' }
    expect(await subjectState({ apiRequest }, owner)).toBe('present')
    expect(apiRequest).toHaveBeenCalledWith('GET', '/api/bot-accounts/qqofficial/id%3Awith%2Fslash')
    apiRequest.mockRejectedValueOnce(new PluginUIError('platform.resource_not_found', 'missing', 404))
    expect(await subjectState({ apiRequest }, owner)).toBe('outside')
    apiRequest.mockRejectedValueOnce(new PluginUIError('platform.internal_error', 'failed', 500))
    await expect(subjectState({ apiRequest }, owner)).rejects.toThrow('failed')
    expect(await subjectState({ apiRequest }, { ...owner, bot_id: '' })).toBe('pending')
    expect(await subjectState({ apiRequest })).toBe('unknown')
  })
  it('loads all OneBot assignment choices including accounts without connections', async () => {
    const first = { source_protocol: 'onebot11', bot_id: 'first', state: 'online' }
    const second = { source_protocol: 'onebot11', bot_id: 'second', state: 'no_connection' }
    const apiRequest = vi.fn().mockResolvedValueOnce({ items: [first], next_cursor: '100' }).mockResolvedValueOnce({ items: [second] })
    expect(await assignmentBots({ apiRequest })).toEqual([first, second])
    expect(apiRequest.mock.calls[1][1]).toContain('cursor=100')
    expect(apiRequest.mock.calls.every(([, path]) => path.includes('source_protocol=onebot11'))).toBe(true)
  })
})
