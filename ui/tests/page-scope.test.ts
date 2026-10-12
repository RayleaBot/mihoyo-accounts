import { flushPromises, shallowMount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import App from '../src/App.vue'
import HistoryRecords from '../src/HistoryRecords.vue'

const state = vi.hoisted(() => ({
  page: { value: { id: 'accounts', scope: 'bot_account' } },
  bot: { value: { source_protocol: 'onebot11', bot_id: 'bot-A', state: 'no_connection' } as unknown },
  invokeAction: vi.fn(),
}))
vi.mock('@rayleabot/plugin-ui', async importOriginal => ({
  ...await importOriginal<typeof import('@rayleabot/plugin-ui')>(),
  usePluginHost: () => ({ ready: Promise.resolve(), loading: { value: false }, page: state.page, bot: state.bot, client: { invokeAction: state.invokeAction } }),
}))
afterEach(() => state.invokeAction.mockReset())

it('loads only the scoped list and starts login using a chat user on a disconnected bot', async () => {
  state.invokeAction.mockImplementation(async (action: string) => {
    if (action === 'status') return { vault_state: 'unlocked', allowed_plugins: [], native_key_available: true }
    if (action === 'accounts.list') return { items: [], total: 0, next_page: null }
    if (action === 'login.start') return { login: { ref: 'login', state: 'failed', role_states: {} } }
    throw Error(`unexpected action ${action}`)
  })
  const wrapper = shallowMount(App)
  try {
    await flushPromises()
    expect(state.invokeAction.mock.calls.map(([action]) => action)).toEqual(['status', 'accounts.list'])
    expect(wrapper.findComponent({ name: 'AccountMaintenance' }).exists()).toBe(false)
    expect(wrapper.findComponent({ name: 'KeyProtection' }).exists()).toBe(false)
    await wrapper.findAll('button').find(b => b.text() === '扫码添加账号')!.trigger('click')
    expect(wrapper.find('select').exists()).toBe(false)
    await wrapper.get('.login-form input').setValue('chat-user')
    await wrapper.get('.login-form').trigger('submit')
    await flushPromises()
    expect(state.invokeAction).toHaveBeenCalledWith('login.start', { actor_id: 'chat-user' })
  } finally { wrapper.unmount() }
})

it('finishes historical scans when Go returns a null next page', async () => {
  const invokeAction = vi.fn(async (action: string) => {
    if (action === 'maintenance.list') return { items: [], next_page: null }
    if (action === 'history.associations.list' || action === 'history.delegations.list') return { items: [] }
    throw Error(`unexpected action ${action}`)
  })
  const client = { invokeAction, reloadSecretStatus: vi.fn(async () => ({ configured: {} })) }
  const wrapper = shallowMount(HistoryRecords, { props: { client: client as never, disabled: false } })
  try {
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(invokeAction.mock.calls.map(([action]) => action)).toEqual(['maintenance.list', 'history.delegations.list', 'history.associations.list'])
    expect(wrapper.get('button').attributes('disabled')).toBeUndefined()
  } finally { wrapper.unmount() }
})
