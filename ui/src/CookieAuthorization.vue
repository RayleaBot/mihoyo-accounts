<script setup lang="ts">
import { onUnmounted, ref } from 'vue'
import type { Account } from './model'
import { sealCredentialInput } from './credential-input'
interface Job { ref: string; state: string; sequence: number; role_states: Record<string, string>; account?: Account }
const props = defineProps<{ busy: boolean; ownerLabel: string; invoke: <T>(action: string, payload?: Record<string, unknown>) => Promise<T> }>()
const emit = defineEmits<{ changed: []; working: [boolean] }>()
const actor = ref(''), value = ref(''), realm = ref<'cn' | 'os'>('cn'), job = ref<Job | null>(null), working = ref(false), error = ref(''), notice = ref(''), stopping = ref(false)
let disposed = false
async function run(resume = false) {
  if (working.value || props.busy) return
  working.value = true; stopping.value = false; emit('working', true); error.value = ''; notice.value = ''
  try {
    if (!resume) {
      if (!actor.value.trim()) throw Error('请填写本人聊天用户ID。')
      const { public_key } = await props.invoke<{ public_key: JsonWebKey }>('vault.public_key')
      const box = await sealCredentialInput(value.value.trim(), public_key, 'account.cookie.import')
      value.value = ''
      job.value = (await props.invoke<{ import: Job }>('accounts.import.start', { actor_id: actor.value.trim(), box, realm: realm.value })).import
    }
    while (job.value?.state === 'discovering' && !disposed && !stopping.value) {
      job.value = (await props.invoke<{ import: Job }>('accounts.import.step', { ref: job.value.ref, sequence: job.value.sequence })).import
    }
    if (job.value?.state === 'bound') { notice.value = (realm.value === 'os' ? 'HoYoLab' : '米游社') + '账号已加密保存，各游戏可共用此授权。'; emit('changed') }
    else if (stopping.value || disposed) { await props.invoke('accounts.import.cancel', { ref: job.value?.ref }).catch(() => {}); job.value = null; notice.value = 'Cookie 导入已停止。' }
  } catch (e) { error.value = e instanceof Error ? e.message : 'Cookie 导入未完成，可重试当前步骤。' }
  finally { value.value = ''; working.value = false; emit('working', false) }
}
async function cancel() { stopping.value = true; if (working.value) return; if (job.value) { await props.invoke('accounts.import.cancel', { ref: job.value.ref }).catch(() => {}); job.value = null } }
onUnmounted(() => { disposed = true; value.value = ''; void cancel() })
</script>

<template>
  <div class="cookie-import">
    <p class="hint">国服也可用米游社扫码。这里加密导入本人的米游社或 HoYoLab Cookie，一次授权供三个游戏共用；原文不会进入游戏插件或聊天。</p>
    <form @submit.prevent="run()">
      <fieldset :disabled="busy || working">
        <legend class="sr-only">绑定到本人聊天身份</legend>
        <label>{{ ownerLabel }}<input v-model="actor" maxlength="256" required autocomplete="off" spellcheck="false"></label>
        <label>地区<select v-model="realm"><option value="cn">国服（米游社）</option><option value="os">海外（HoYoLab）</option></select></label>
        <label>本人 Cookie<input v-model="value" type="password" autocomplete="off" maxlength="8192" required aria-describedby="cookie-help"></label>
        <p id="cookie-help" class="hint">{{ realm === 'cn' ? '国服需要 SToken（stuid、stoken 与 mid），或 LToken 与 CookieToken。' : '海外需要账号 ID 与 LToken，或 SToken v2 与 MID 以换取凭据；完整抽卡等业务还需要 CookieToken v2 和 MID。' }}</p>
        <div class="actions"><button class="primary" :disabled="!actor.trim() || !value.trim() || job?.state === 'discovering'">{{ working ? '正在导入…' : '加密导入' }}</button></div>
      </fieldset>
    </form>
    <p v-if="error" role="alert" class="danger-text">{{ error }}</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <p v-if="job" class="progress">{{ job.state === 'bound' ? '已保存' : '角色发现进度' }} · <span class="num">{{ Object.keys(job.role_states).length }} / 3</span></p>
    <div v-if="job?.state === 'discovering'" class="actions">
      <button v-if="!working" type="button" :disabled="busy" @click="run(true)">重试当前步骤</button>
      <button type="button" :disabled="stopping" @click="cancel">停止导入</button>
    </div>
  </div>
</template>

<style scoped>
.cookie-import, .cookie-import fieldset { display: grid; gap: 14px; min-width: 0; }
.cookie-import p { margin: 0; }
.progress { color: var(--ink-2); }
</style>
