<script setup lang="ts">
import { ref } from 'vue'
import Icon from './Icon.vue'
import type { Account } from './model'
import { sealCredentialInput } from './credential-input'
const props = defineProps<{ account: Account; busy: boolean; invoke: <T>(action: string, payload?: Record<string, unknown>) => Promise<T> }>()
const emit = defineEmits<{ changed: []; working: [boolean] }>()
const game = ref('genshin'), value = ref(''), working = ref(false), error = ref(''), notice = ref(''), clearing = ref(false)
async function run(fn: () => Promise<void>) { if (working.value || props.busy) return; working.value = true; emit('working', true); error.value = ''; notice.value = ''; try { await fn(); emit('changed') } catch (e) { error.value = e instanceof Error ? e.message : '云授权操作未完成。' } finally { value.value = ''; working.value = false; emit('working', false) } }
async function save() { await run(async () => { const { public_key } = await props.invoke<{ public_key: JsonWebKey }>('vault.public_key'); const box = await sealCredentialInput(value.value.trim(), public_key, 'account.cloud.save'); value.value = ''; await props.invoke('accounts.cloud.save', { account_ref: props.account.ref, revision: props.account.revision, game: game.value, box }); notice.value = '云授权已加密保存。' }) }
async function clear() { await run(async () => { await props.invoke('accounts.cloud.clear', { account_ref: props.account.ref, revision: props.account.revision, game: game.value }); clearing.value = false; notice.value = '所选云授权已移除。' }) }
</script>

<template>
  <details class="disclosure">
    <summary><Icon name="cloud" :size="18" /><span>云游戏独立授权</span><span class="summary-state">{{ account.cloud_configured?.length ? '已配置 ' + account.cloud_configured.map(g => g === 'genshin' ? '云原神' : '云绝区零').join('、') : '未配置' }}</span><Icon class="chevron" name="chevron" :size="16" /></summary>
    <div class="disclosure-body">
      <p class="hint">云游戏使用独立的客户端授权，普通米游社 CK 可继续供三游戏共用。只为此账号保存对应云 Token，浏览器先加密，再写入加密账号库；不会回显，也不在聊天收取。</p>
      <p v-if="error" role="alert" class="danger-text">{{ error }}</p>
      <p v-if="notice" role="status">{{ notice }}</p>
      <form @submit.prevent="save">
        <fieldset :disabled="busy || working">
          <legend class="sr-only">配置账号 {{ account.account_label }} 的云授权</legend>
          <label>云游戏<select v-model="game" @change="clearing = false"><option value="genshin">云原神</option><option value="zzz">云绝区零</option></select></label>
          <label>云客户端 combo_token<input v-model="value" type="password" autocomplete="off" maxlength="8192" required></label>
          <div class="actions"><button class="primary" :disabled="!value.trim()">加密保存云授权</button><button type="button" :disabled="!account.cloud_configured?.includes(game)" @click="clearing = true">移除所选云授权</button></div>
        </fieldset>
      </form>
      <div v-if="clearing" class="confirm-strip">
        <p>移除所选云授权后，相应云游戏任务将暂停；普通米游社 CK 保留。</p>
        <div class="actions"><button type="button" :disabled="busy || working" @click="clearing = false">保留云授权</button><button type="button" class="destructive" :disabled="busy || working" @click="clear">确认移除云授权</button></div>
      </div>
    </div>
  </details>
</template>
