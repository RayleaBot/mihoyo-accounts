import { flushPromises, shallowMount } from '@vue/test-utils'
import { expect, it, vi } from 'vitest'
import App from '../src/App.vue'

const invokeAction = vi.hoisted(() => vi.fn())
vi.mock('@rayleabot/plugin-ui', () => ({
  usePluginHost: () => ({ ready: Promise.resolve(), loading: { value: false }, client: { invokeAction } }),
}))

it('shows native key recovery guidance and accepts a restored password key after refreshing', async () => {
  const guidance = 'synthetic key recovery instructions'
  let status = {
    vault_state: 'locked', key_protection: 'dpapi', native_key_available: true, bots: [], allowed_plugins: [],
    vault_error: { code: 'plugin.vault_locked', message: guidance, details: { reason: 'native_key_unavailable' } },
  }
  invokeAction.mockImplementation(async (action: string) => {
    if (action === 'status') return status
    throw new Error(`unexpected action: ${action}`)
  })
  const wrapper = shallowMount(App)
  try {
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe(guidance)
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)

    status = { ...status, key_protection: 'password' }
    await wrapper.get('form button[type="button"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('input[type="password"]').attributes('autocomplete')).toBe('current-password')
    expect(invokeAction.mock.calls.map(([action]) => action)).toEqual(['status', 'status'])
  } finally {
    wrapper.unmount()
    invokeAction.mockReset()
  }
})
