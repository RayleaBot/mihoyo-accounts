<script setup lang="ts">
import { computed, ref } from 'vue'
import Icon from './Icon.vue'
import { encryptPassword, errorText, protectionNames } from './model'

const props = defineProps<{
  invoke: <T>(action: string, payload?: Record<string, unknown>) => Promise<T>
  protection: string
  nativeAvailable: boolean
  busy: boolean
}>()
const emit = defineEmits<{ changed: []; working: [boolean] }>()

const target = ref(props.protection === 'dpapi' ? 'password' : 'dpapi')
const current = ref('')
const next = ref('')
const confirmation = ref('')
const working = ref(false)
const error = ref('')
const notice = ref('')

const choices = computed(() => props.nativeAvailable ? ['password', 'dpapi'] : ['password'])
const needsCurrent = computed(() => props.protection === 'password')
const needsNext = computed(() => target.value === 'password')
const unchanged = computed(() => target.value === 'dpapi' && props.protection === 'dpapi')

async function submit() {
  if (working.value || props.busy || unchanged.value) return
  if (needsNext.value && next.value !== confirmation.value) {
    error.value = '两次输入的新口令不一致。'
    return
  }
  working.value = true
  emit('working', true)
  error.value = ''
  notice.value = ''
  try {
    const { public_key } = await props.invoke<{ public_key: JsonWebKey }>('vault.public_key')
    const payload: Record<string, unknown> = { mode: target.value }
    if (needsCurrent.value) payload.password_ciphertext = await encryptPassword(current.value, public_key)
    if (needsNext.value) payload.new_password_ciphertext = await encryptPassword(next.value, public_key)
    await props.invoke('vault.mode', payload)
    notice.value = target.value === 'password'
      ? '已改用恢复口令保护。重启插件后需输入新口令解锁。'
      : '已改用当前 Windows 用户保护，插件启动时自动解锁。'
    emit('changed')
  } catch (cause) {
    error.value = errorText(cause)
  } finally {
    current.value = ''
    next.value = ''
    confirmation.value = ''
    working.value = false
    emit('working', false)
  }
}
</script>

<template>
  <section class="key-protection" aria-labelledby="key-protection-heading">
    <h3 id="key-protection-heading">密钥保护方式</h3>
    <p>使用{{ protectionNames[protection] || '未知方式' }}保护</p>
    <p class="hint">切换只重新包装同一把密钥，已保存的账号不会重新加密。Windows 用户方式在启动时自动解锁，但同一台电脑上以相同用户运行的程序都能解开；恢复口令需要每次启动后手动解锁。</p>
    <p v-if="error" class="danger-text" role="alert">{{ error }}</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <details class="disclosure">
      <summary><Icon name="key" :size="18" /><span>更改保护方式</span><Icon class="chevron" name="chevron" :size="16" /></summary>
      <form class="disclosure-body" @submit.prevent="submit">
        <fieldset :disabled="working || busy">
          <legend class="sr-only">切换密钥保护方式</legend>
          <label>改为
            <select v-model="target">
              <option v-for="choice in choices" :key="choice" :value="choice">{{ protectionNames[choice] }}</option>
            </select>
          </label>
          <label v-if="needsCurrent">当前恢复口令
            <input v-model="current" type="password" autocomplete="current-password" required>
          </label>
          <template v-if="needsNext">
            <label>{{ protection === 'password' ? '新恢复口令' : '恢复口令' }}
              <input v-model="next" type="password" autocomplete="new-password" minlength="12" maxlength="128" required>
            </label>
            <label>再次输入
              <input v-model="confirmation" type="password" autocomplete="new-password" required>
            </label>
            <p class="hint">12 到 128 字节。丢失口令后无法恢复账号。</p>
          </template>
          <div class="actions"><button type="submit" :disabled="unchanged">{{ working ? '正在切换…' : '切换保护方式' }}</button></div>
        </fieldset>
      </form>
    </details>
  </section>
</template>

<style scoped>
.key-protection, .key-protection fieldset { display: grid; gap: 14px; min-width: 0; }
</style>
