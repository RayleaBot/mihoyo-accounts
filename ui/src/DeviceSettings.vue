<script setup lang="ts">
import { ref } from 'vue'
import Icon from './Icon.vue'
import { shortDate, type Account } from './model'
import { sealCredentialInput } from './credential-input'
const props = defineProps<{ account: Account; busy: boolean; invoke: <T>(action: string, payload?: Record<string, unknown>) => Promise<T> }>()
const emit = defineEmits<{ changed: []; working: [boolean] }>()
const state = ref<{ revision: number; device_label: string; has_fingerprint: boolean; fingerprint_expires_ms: number; fingerprint_imported: boolean; device_model?: string } | null>(null), device = ref(''), working = ref(false), error = ref(''), notice = ref(''), value = ref(''), confirm = ref(false)
async function run(work: () => Promise<void>) { if (working.value || props.busy) return; working.value = true; emit('working', true); error.value = ''; notice.value = ''; try { await work() } catch (e) { error.value = e instanceof Error ? e.message : '设备操作未完成。' } finally { value.value = ''; working.value = false; emit('working', false) } }
async function load() { await run(async () => { state.value = await props.invoke('accounts.device.status', { account_ref: props.account.ref }); confirm.value = false }) }
async function reset() { if (!confirm.value || !state.value) return; await run(async () => { let box; if (value.value.trim()) { const { public_key } = await props.invoke<{ public_key: JsonWebKey }>('vault.public_key'); box = await sealCredentialInput(value.value.trim(), public_key, 'account.device.reset'); value.value = '' } state.value = await props.invoke('accounts.device.reset', { account_ref: props.account.ref, revision: state.value!.revision, confirm: true, ...(box ? { box } : {}) }); confirm.value = false; notice.value = '设备标识已更新，下次业务查询会重新获取官方指纹。'; emit('changed') }) }
async function importDevice() { if (!state.value || !device.value.trim()) return; await run(async () => { const { public_key } = await props.invoke<{ public_key: JsonWebKey }>('vault.public_key'); const box = await sealCredentialInput(device.value.trim(), public_key, 'account.device.import'); device.value = ''; state.value = await props.invoke('accounts.device.import', { account_ref: props.account.ref, revision: state.value!.revision, box }); notice.value = '设备已绑定，之后的查询使用该设备。'; emit('changed') }) }
</script>

<template>
  <details class="disclosure">
    <summary><Icon name="device" :size="18" /><span>设备信息与重置</span><Icon class="chevron" name="chevron" :size="16" /></summary>
    <div class="disclosure-body">
      <p class="hint">显示完整的设备标识；重置仅更新此账号插件使用的设备标识并清除缓存指纹，不撤销官方应用设备。官方设备验证仍需在官方应用处理。</p>
      <div class="actions"><button type="button" :disabled="busy || working" @click="load">读取设备状态</button></div>
      <p v-if="error" class="danger-text" role="alert">{{ error }}</p>
      <p v-if="notice" role="status">{{ notice }}</p>
      <dl v-if="state" class="facts">
        <div><dt>设备</dt><dd><span v-if="state.device_label" class="code">{{ state.device_label }}</span><template v-else>未设置</template><template v-if="state.device_model"> · {{ state.device_model }}</template></dd></div>
        <div><dt>指纹</dt><dd>{{ state.fingerprint_imported ? '已导入' : state.has_fingerprint ? '已缓存' : '未缓存' }}<template v-if="state.has_fingerprint && !state.fingerprint_imported"> · {{ shortDate(state.fingerprint_expires_ms) }} 到期</template></dd></div>
      </dl>
      <form v-if="state" @submit.prevent="reset">
        <label>自定义设备标识（留空自动生成）<input v-model="value" type="text" autocomplete="off" spellcheck="false" maxlength="64" :disabled="busy || working"></label>
        <label class="check"><input v-model="confirm" type="checkbox" :disabled="busy || working"><span>确认更新此账号的设备标识并清除指纹</span></label>
        <div class="actions"><button :disabled="busy || working || !confirm">重置设备信息</button></div>
      </form>
      <form v-if="state" @submit.prevent="importDevice">
        <label>绑定设备（出现 10041 时使用）<textarea v-model="device" rows="3" autocomplete="off" spellcheck="false" maxlength="8192" :disabled="busy || working" aria-describedby="device-help"></textarea></label>
        <p id="device-help" class="hint">粘贴 {"device_id": "…", "device_fp": "…"}，或设备信息工具复制的内容。</p>
        <div class="actions"><button :disabled="busy || working || !device.trim()">加密导入设备</button></div>
      </form>
    </div>
  </details>
</template>
