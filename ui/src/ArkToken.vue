<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { sealCredentialInput } from './credential-input'
import { errorText } from './model'

const props = defineProps<{
  invoke: <T>(action: string, payload?: Record<string, unknown>) => Promise<T>
  busy: boolean
}>()

const configured = ref(false)
const token = ref('')
const working = ref(false)
const error = ref('')
const notice = ref('')

async function run(task: () => Promise<{ configured: boolean }>, done: string) {
  if (working.value || props.busy) return
  working.value = true
  error.value = ''
  notice.value = ''
  try {
    configured.value = (await task()).configured
    notice.value = done
  } catch (cause) {
    error.value = errorText(cause)
  } finally {
    token.value = ''
    working.value = false
  }
}

async function save() {
  await run(async () => {
    const { public_key } = await props.invoke<{ public_key: JsonWebKey }>('vault.public_key')
    const box = await sealCredentialInput(token.value.trim(), public_key, 'ark.token.save')
    return props.invoke<{ configured: boolean }>('ark.save', { box })
  }, 'ark 令牌已加密保存。')
}

async function remove() {
  await run(() => props.invoke<{ configured: boolean }>('ark.clear'), 'ark 令牌已清除。')
}

onMounted(async () => {
  try {
    configured.value = (await props.invoke<{ configured: boolean }>('ark.status')).configured
  } catch (cause) {
    error.value = errorText(cause)
  }
})
</script>

<template>
  <section class="ark-token" aria-labelledby="ark-token-heading">
    <h3 id="ark-token-heading">ark 授权令牌</h3>
    <p class="hint">
      原神与星铁插件的云排名、高级筛选和云面板授权查询使用 ark（ark.ivny.cn）令牌。令牌与账号一样加密保存，只由本插件发送给 ark，游戏插件拿不到它。
      当前：{{ configured ? '已配置' : '未配置' }}。
    </p>
    <p v-if="error" class="danger-text" role="alert">{{ error }}</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <form @submit.prevent="save">
      <fieldset :disabled="working || busy">
        <legend class="sr-only">保存 ark 授权令牌</legend>
        <label>{{ configured ? '替换令牌' : '令牌' }}
          <input v-model="token" type="password" autocomplete="off" minlength="8" maxlength="4096" required>
        </label>
        <div class="actions">
          <button type="submit">{{ working ? '正在保存…' : '加密保存' }}</button>
          <button v-if="configured" type="button" @click="remove">清除令牌</button>
        </div>
      </fieldset>
    </form>
  </section>
</template>

<style scoped>
.ark-token, .ark-token fieldset { display: grid; gap: 14px; min-width: 0; }
</style>
