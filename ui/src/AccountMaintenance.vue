<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref } from 'vue'
import { driveBatch, type Batch, type Invoke } from './batch'
import { errorText, gameNames, type Account } from './model'
const props = defineProps<{ selected: Account[]; disabled: boolean; invoke: Invoke }>()
const emit = defineEmits<{ working: [value: boolean]; changed: [] }>()
const busy = ref(false), error = ref(''), batch = ref<Batch | null>(null), labels = ref<Record<string, string>>({})
const confirmation = ref<HTMLDialogElement | null>(null), candidates = ref<{ account_ref: string; revision: number; label: string }[]>([])
const active = computed(() => batch.value?.state === 'running')
const invalid = computed(() => props.selected.filter(a => a.status === 'invalid'))
const modeNames = { check: '检查登录状态', refresh: '刷新凭据', remove_invalid: '清理失效账号', statistics: '统计账号' }
let controller: AbortController | undefined, disposed = false
function label(account: Account) { return `${account.account_label} · ${account.owner.actor_id} · ${account.owner.source_adapter}` }
function time(ms?: number) { return ms ? new Date(ms).toLocaleString('zh-CN') : '尚未完成' }
function finishedWork() { busy.value = false; emit('working', false) }
async function perform(mode?: Batch['mode'], items?: { account_ref: string; revision: number }[]) {
  if (busy.value || props.disabled) return
  busy.value = true; error.value = ''; emit('working', true); controller = new AbortController()
  const signal = controller.signal
  try {
    if (mode) {
      if (batch.value) await props.invoke('accounts.batch.cancel', { ref: batch.value.ref }).catch(() => {})
      labels.value = Object.fromEntries(props.selected.map(a => [a.ref, label(a)]))
      batch.value = (await props.invoke<{ batch: Batch }>('accounts.batch.start', { mode, items: mode === 'statistics' ? [] : items ?? props.selected.map(a => ({ account_ref: a.ref, revision: a.revision })), confirm: true })).batch
    }
    if (!batch.value) return
    if (disposed) controller.abort()
    const current = await driveBatch(props.invoke, batch.value, signal, value => { if (!disposed) batch.value = value })
    if (!disposed) batch.value = current
    if (current.mode !== 'statistics') emit('changed')
  } catch (cause) {
    if (!disposed) error.value = errorText(cause)
    if (batch.value && batch.value.mode !== 'statistics') emit('changed')
  } finally { controller = undefined; finishedWork() }
}
async function cancel() {
  if (controller) { controller.abort(); return }
  if (!batch.value || busy.value) return
  busy.value = true; error.value = ''; emit('working', true)
  try { batch.value = (await props.invoke<{ batch: Batch }>('accounts.batch.cancel', { ref: batch.value.ref })).batch }
  catch (cause) { error.value = errorText(cause) }
  finally { finishedWork() }
}
async function askRemove() {
  candidates.value = invalid.value.map(a => ({ account_ref: a.ref, revision: a.revision, label: label(a) }))
  await nextTick(); confirmation.value?.showModal()
}
async function remove() {
  const items = candidates.value.map(({ account_ref, revision }) => ({ account_ref, revision }))
  confirmation.value?.close(); await perform('remove_invalid', items)
}
onUnmounted(() => { disposed = true; if (controller) controller.abort(); else if (batch.value) void props.invoke('accounts.batch.cancel', { ref: batch.value.ref }).catch(() => {}) })
</script>
<template>
  <section class="account-maintenance" aria-labelledby="maintenance-heading">
    <div class="section-heading"><h3 id="maintenance-heading">批量维护与统计</h3><span class="muted">当前页选中 {{ selected.length }} 个账号</span></div>
    <p class="hint">检查通过官方角色绑定接口验证已保存登录状态；刷新会换取新凭据。每批最多 128 个账号，开始后范围固定。关闭页面停止后续处理，已完成的操作保留。</p>
    <div class="actions">
      <button :disabled="disabled || busy || active || !selected.length" @click="perform('check')">检查所选账号</button>
      <button :disabled="disabled || busy || active || !selected.length" @click="perform('refresh')">刷新所选凭据</button>
      <button class="danger-text" :disabled="disabled || busy || active || !invalid.length" @click="askRemove">清理所选失效账号（{{ invalid.length }}）</button>
      <button :disabled="disabled || busy || active" @click="perform('statistics')">统计全部账号</button>
    </div>
    <p v-if="error" role="alert" class="feedback danger">{{ error }}</p>
    <div v-if="batch" class="batch-result">
      <p role="status"><strong>{{ modeNames[batch.mode] }}</strong> · {{ batch.state === 'completed' ? '已完成' : batch.state === 'canceled' ? '已停止' : busy ? '处理中' : '已暂停，可继续' }} · 已处理 {{ batch.processed }}<template v-if="batch.mode !== 'statistics'"> / {{ batch.total }}</template> 个账号</p>
      <div v-if="active" class="actions"><button v-if="!busy" :disabled="disabled" @click="perform()">继续本批任务</button><button @click="cancel">停止剩余操作</button></div>
      <p v-if="batch.state !== 'running'" class="hint">{{ time(batch.started_at_ms) }} — {{ time(batch.finished_at_ms) }}（本地时间）</p>
      <ul v-if="batch.results.length" class="batch-items"><li v-for="(item, index) in batch.results" :key="index"><strong>{{ labels[item.account_ref] || item.label }}</strong><span>{{ item.state === 'success' ? '成功' : item.state === 'skipped' ? '已跳过' : '未完成' }}</span><p v-if="item.message" class="hint">{{ item.message }}</p></li></ul>
      <template v-if="batch.statistics">
        <p class="hint">读取了 {{ batch.statistics.pages }} 页保存摘要；分页期间账号可能变化，此结果不是同一时刻快照。统计不会访问米游社或刷新凭据。</p>
        <dl class="statistics"><div><dt>本轮读取账号</dt><dd>{{ batch.statistics.accounts }}</dd></div><div><dt>聊天主体（含机器人归属）</dt><dd>{{ batch.statistics.subjects }}</dd></div><div><dt>保存状态可用</dt><dd>{{ batch.statistics.states.valid || 0 }}</dd></div><div><dt>保存状态失效</dt><dd>{{ batch.statistics.states.invalid || 0 }}</dd></div><div><dt>待检查</dt><dd>{{ batch.statistics.states.unknown || 0 }}</dd></div><div><dt>具备刷新材料</dt><dd>{{ batch.statistics.refreshable }}</dd></div><div><dt>主动开启公共查询</dt><dd>{{ batch.statistics.public_queries }}</dd></div><div v-for="(name, game) in gameNames" :key="game"><dt>{{ name }}保存角色</dt><dd>{{ batch.statistics.game_roles[game] || 0 }}</dd></div></dl>
      </template>
    </div>
    <dialog ref="confirmation" aria-labelledby="cleanup-title"><h2 id="cleanup-title">清理 {{ candidates.length }} 个失效账号</h2><p>这些账号将被移除，游戏插件将无法继续使用。刚重新登录、已恢复可用或期间发生变化的账号会跳过。</p><ul><li v-for="item in candidates" :key="item.account_ref">{{ item.label }}</li></ul><div class="actions"><button autofocus @click="confirmation?.close()">取消</button><button class="destructive" @click="remove">确认清理所列失效账号</button></div></dialog>
  </section>
</template>
<style scoped>
.account-maintenance{display:grid;gap:16px;padding:20px 0;border-bottom:1px solid var(--raylea-color-border)}.account-maintenance .section-heading{margin:0;flex-wrap:wrap}.batch-result{display:grid;gap:12px}.batch-items{list-style:none;margin:0;padding:0;max-height:360px;overflow:auto;scrollbar-color:var(--control-border) var(--raylea-color-bg);scrollbar-width:thin}.batch-items li{display:flex;gap:8px 16px;flex-wrap:wrap;padding:12px 0;border-bottom:1px solid var(--raylea-color-border)}.batch-items p{flex-basis:100%}.statistics{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:0 24px;margin:0}.statistics>div{display:flex;justify-content:space-between;gap:12px;padding:10px 0;border-bottom:1px solid var(--raylea-color-border)}.statistics dd{margin:0;font-variant-numeric:tabular-nums}dialog ul{padding-left:20px;max-height:240px;overflow:auto}dialog p{margin:16px 0}@media(max-width:720px){.statistics{grid-template-columns:1fr}}
</style>
