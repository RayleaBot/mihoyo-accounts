<script setup lang="ts">
import { ref } from 'vue'
import { encryptPassword, errorText } from './model'
import { canonicalBackup, downloadBackup, uploadBackup, type Invoke } from './migration'
const props = defineProps<{ invoke: Invoke; protection: string; busy: boolean }>()
const emit = defineEmits<{ changed: []; working: [boolean] }>()
const working = ref(false), error = ref(''), notice = ref(''), passphrase = ref(''), currentPassword = ref('')
const imported = ref(''), importName = ref(''), confirmRestore = ref(false)
async function wrap(value: string) { const { public_key } = await props.invoke<{ public_key: JsonWebKey }>('vault.public_key'); return encryptPassword(value, public_key) }
async function run(action: () => Promise<void>) {
  if (working.value || props.busy) return
  working.value = true; emit('working', true); error.value = ''; notice.value = ''
  try { await action() } catch (cause) { error.value = errorText(cause) }
  finally { passphrase.value = ''; currentPassword.value = ''; working.value = false; emit('working', false) }
}
async function exportFile() { await run(async () => {
  const password = await wrap(passphrase.value); passphrase.value = ''
  const content = await downloadBackup(props.invoke, password)
  const url = URL.createObjectURL(new Blob([content], { type: 'application/json' })); const a = document.createElement('a'); a.href = url; a.download = `mihoyo-accounts-encrypted-${new Date().toISOString().slice(0, 10)}.json`; a.click(); setTimeout(() => URL.revokeObjectURL(url), 1000)
  notice.value = '加密备份已下载，请保存好独立的备份口令。'
}) }
async function readFile(event: Event) {
  const element = event.target as HTMLInputElement, file = element.files?.[0]
  imported.value = ''; importName.value = ''; confirmRestore.value = false; error.value = ''
  if (!file) return
  try { if (file.size > 16 * 1024 * 1024) throw new Error('备份文件超过 16 MiB。'); imported.value = canonicalBackup(await file.text()); importName.value = file.name }
  catch (cause) { error.value = errorText(cause) }
  finally { element.value = '' }
}
async function restore() { if (!confirmRestore.value || !imported.value) return; await run(async () => {
  const password = await wrap(passphrase.value); passphrase.value = ''
  const count = await uploadBackup(props.invoke, imported.value, password)
  imported.value = ''; importName.value = ''; confirmRestore.value = false; emit('changed'); notice.value = `已恢复 ${count} 个账号，并使用本机账号库密钥重新加密。`
}) }
async function reencrypt() { let page: number | null = 0; while (page !== null) { const result: { next_page: number | null } = await props.invoke('vault.reencrypt', { page }); if (result.next_page !== null && (!Number.isInteger(result.next_page) || result.next_page <= page)) throw new Error('重加密进度无效，请重试。'); page = result.next_page } }
async function rotate() { await run(async () => {
  const password = props.protection === 'password' ? await wrap(currentPassword.value) : ''
  currentPassword.value = ''
  await props.invoke('vault.rotate', password ? { password_ciphertext: password } : {})
  notice.value = '新密钥已启用，正在重加密活动记录。'
  await reencrypt(); emit('changed'); notice.value = '新密钥已启用，活动记录已重加密。'
}) }
async function resume() { await run(async () => { await reencrypt(); notice.value = '活动记录已重加密。' }) }
</script>
<template>
  <section class="vault-maintenance">
    <h2>加密备份与迁移</h2><p class="hint">备份可用于新机器恢复，仅包含加密内容。备份口令独立于账号库口令，遗失后无法解密；恢复会保留原聊天用户和机器人归属。</p>
    <p v-if="error" role="alert" class="error">{{ error }}</p><p v-if="notice" role="status">{{ notice }}</p>
    <fieldset :disabled="working || busy"><legend class="sr-only">账号备份</legend>
      <label>备份口令<input v-model="passphrase" type="password" autocomplete="new-password" placeholder="12–128 字节"></label>
      <div class="actions"><button type="button" @click="exportFile">下载加密备份</button><label class="file-picker">选择加密备份<input type="file" accept=".json,application/json" @change="readFile"></label></div>
      <template v-if="imported"><p>{{ importName }}</p><label class="check"><input v-model="confirmRestore" type="checkbox">恢复到没有活动账号的目标库，并保留备份中的账号归属</label><button type="button" :disabled="!confirmRestore" @click="restore">恢复加密备份</button></template>
    </fieldset>
    <details><summary>密钥轮换与中断恢复</summary><p class="hint">新记录使用新密钥；旧密钥继续受保护，用于读取历史密文。轮换不使外部旧副本失效。重加密中断后可继续。</p>
      <fieldset :disabled="working || busy"><legend class="sr-only">密钥轮换</legend><label v-if="protection === 'password'">当前账号库口令<input v-model="currentPassword" type="password" autocomplete="current-password"></label><div class="actions"><button type="button" @click="rotate">轮换密钥并重加密</button><button type="button" @click="resume">继续重加密</button></div></fieldset>
    </details>
  </section>
</template>
<style scoped>
.vault-maintenance{display:grid;gap:16px;padding-top:24px;border-top:1px solid var(--raylea-color-border,#ddd)}.vault-maintenance fieldset{border:0;min-width:0;padding:0;margin:0;display:grid;gap:16px}.vault-maintenance label:not(.check){display:grid;gap:6px;max-width:560px}.vault-maintenance .hint{max-width:74ch}.vault-maintenance details>p,.vault-maintenance details>fieldset{margin-top:16px}.file-picker{font-size:13px}.sr-only{position:absolute;width:1px;height:1px;overflow:hidden;clip-path:inset(50%)}
</style>
