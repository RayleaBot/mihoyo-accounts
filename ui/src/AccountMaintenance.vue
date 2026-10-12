<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref } from 'vue'
import Icon from './Icon.vue'
import { driveBatch, type Batch, type Invoke } from './batch'
import { errorText, shortDate, type Account } from './model'
const props = defineProps<{ selected: Account[]; disabled: boolean; invoke: Invoke; describe: (account: Account) => string }>()
const emit = defineEmits<{ working: [value: boolean]; changed: []; clear: [] }>()
const busy = ref(false), error = ref(''), batch = ref<Batch | null>(null), labels = ref<Record<string, string>>({})
const confirmation = ref<HTMLDialogElement | null>(null), candidates = ref<{ account_ref: string; revision: number; label: string }[]>([])
const active = computed(() => batch.value?.state === 'running')
const invalid = computed(() => props.selected.filter(a => a.status === 'invalid'))
const locked = computed(() => props.disabled || busy.value || active.value)
const modeNames = { check: '检查登录状态', refresh: '刷新凭据', remove_invalid: '清理失效账号', statistics: '统计账号' }
const resultNames = { success: '成功', skipped: '已跳过', failed: '未完成' }
const progress = computed(() => batch.value?.total ? batch.value.processed / batch.value.total : 0)
let controller: AbortController | undefined, disposed = false
function time(ms?: number) { return ms ? shortDate(ms) : '尚未完成' }
function finishedWork() { busy.value = false; emit('working', false) }
async function perform(mode?: Exclude<Batch['mode'], 'statistics'>, items?: { account_ref: string; revision: number }[]) {
  if (busy.value || props.disabled) return
  busy.value = true; error.value = ''; emit('working', true); controller = new AbortController()
  const signal = controller.signal
  try {
    if (mode) {
      if (batch.value) await props.invoke('accounts.batch.cancel', { ref: batch.value.ref }).catch(() => {})
      labels.value = Object.fromEntries(props.selected.map(a => [a.ref, props.describe(a)]))
      batch.value = (await props.invoke<{ batch: Batch }>('accounts.batch.start', { mode, items: items ?? props.selected.map(a => ({ account_ref: a.ref, revision: a.revision })), confirm: true })).batch
    }
    if (!batch.value) return
    if (disposed) controller.abort()
    const current = await driveBatch(props.invoke, batch.value, signal, value => { if (!disposed) batch.value = value })
    if (!disposed) batch.value = current
    emit('changed')
  } catch (cause) {
    if (!disposed) error.value = errorText(cause)
    if (batch.value) emit('changed')
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
  candidates.value = invalid.value.map(a => ({ account_ref: a.ref, revision: a.revision, label: props.describe(a) }))
  await nextTick(); confirmation.value?.showModal()
}
async function remove() {
  const items = candidates.value.map(({ account_ref, revision }) => ({ account_ref, revision }))
  confirmation.value?.close(); await perform('remove_invalid', items)
}
defineExpose({ start: (mode: 'check' | 'refresh') => perform(mode) })
onUnmounted(() => { disposed = true; if (controller) controller.abort(); else if (batch.value) void props.invoke('accounts.batch.cancel', { ref: batch.value.ref }).catch(() => {}) })
</script>

<template>
  <section class="maintenance" aria-labelledby="maintenance-heading">
    <h2 id="maintenance-heading" class="sr-only">批量维护</h2>
    <Transition name="dock">
      <div v-show="selected.length || batch" class="dock">
        <div v-if="batch" class="batch">
          <div class="batch-head">
            <p role="status"><strong>{{ modeNames[batch.mode] }}</strong> · {{ batch.state === 'completed' ? '已完成' : batch.state === 'canceled' ? '已停止' : busy ? '处理中' : '已暂停，可继续' }} · 已处理 <span class="num">{{ batch.processed }} / {{ batch.total }}</span> 个账号</p>
            <div class="tool-actions">
              <template v-if="active">
                <button v-if="!busy" type="button" class="quiet" :disabled="disabled" @click="perform()">继续本批任务</button>
                <button type="button" class="quiet" @click="cancel">停止剩余操作</button>
              </template>
              <button v-else type="button" class="quiet" :disabled="busy" @click="batch = null">关闭结果</button>
            </div>
          </div>
          <div class="meter" aria-hidden="true"><span :style="{ inlineSize: `${progress * 100}%` }" /></div>
          <p v-if="batch.state !== 'running'" class="hint">{{ time(batch.started_at_ms) }} — {{ time(batch.finished_at_ms) }}（本地时间）</p>
          <ul v-if="batch.results.length" class="batch-items">
            <li v-for="(item, index) in batch.results" :key="index" :class="item.state">
              <span>{{ labels[item.account_ref] || item.label }}</span>
              <strong class="result">{{ resultNames[item.state] }}</strong>
              <p v-if="item.message" class="hint">{{ item.message }}</p>
            </li>
          </ul>
        </div>
        <p v-if="error" role="alert" class="feedback danger">{{ error }}</p>
        <div v-if="selected.length" class="tools">
          <strong class="picked">已选 <span class="num">{{ selected.length }}</span> 个账号</strong>
          <div class="tool-actions">
            <button type="button" class="quiet" :disabled="locked" @click="perform('check')"><Icon name="shield" :size="16" />检查所选</button>
            <button type="button" class="quiet" :disabled="locked" @click="perform('refresh')"><Icon name="refresh" :size="16" />刷新所选凭据</button>
            <button type="button" class="quiet" :disabled="locked || !invalid.length" @click="askRemove"><Icon name="trash" :size="16" />清理所选失效（{{ invalid.length }}）</button>
          </div>
          <button type="button" class="quiet clear" :disabled="busy" @click="emit('clear')"><Icon name="close" :size="16" />取消选择</button>
          <p class="hint">检查通过官方角色绑定接口验证已保存登录状态；刷新会换取新凭据。每批最多 128 个账号，开始后范围固定。关闭页面停止后续处理，已完成的操作保留。</p>
        </div>
      </div>
    </Transition>

    <dialog ref="confirmation" aria-labelledby="cleanup-title">
      <h2 id="cleanup-title">清理 {{ candidates.length }} 个失效账号</h2>
      <p>这些账号将被移除，游戏插件将无法继续使用。刚重新登录、已恢复可用或期间发生变化的账号会跳过。</p>
      <ul><li v-for="item in candidates" :key="item.account_ref">{{ item.label }}</li></ul>
      <div class="actions"><button type="button" autofocus @click="confirmation?.close()">取消</button><button type="button" class="destructive" @click="remove">确认清理所列失效账号</button></div>
    </dialog>
  </section>
</template>

<style scoped>
/* The batch dock floats at the bottom of the sheet while cards are ticked. */
.maintenance { position: sticky; z-index: 5; bottom: 16px; }
.dock { display: grid; gap: 10px; margin-top: 28px; padding: 10px 12px 12px 16px; border-radius: 14px; background: var(--card); box-shadow: var(--card-shadow), 0 18px 36px -18px rgb(10 16 28 / 32%); }
.dock-enter-active { transition: opacity 200ms ease-out, transform 260ms cubic-bezier(.2, .8, .2, 1); }
.dock-leave-active { transition: opacity 150ms ease-in, transform 150ms ease-in; }
.dock-enter-from, .dock-leave-to { opacity: 0; transform: translateY(18px); }
.tools { display: flex; flex-wrap: wrap; gap: 4px 12px; align-items: center; }
.tools .hint { flex-basis: 100%; }
.picked { font-weight: 650; }
.tool-actions { display: flex; flex-wrap: wrap; gap: 2px; }
.clear { margin-left: auto; }
.batch { display: grid; gap: 10px; max-height: min(32vh, 300px); padding-bottom: 12px; overflow: auto; border-bottom: 1px solid var(--rule); scrollbar-width: thin; }
.batch-head { display: flex; flex-wrap: wrap; gap: 8px 16px; align-items: center; justify-content: space-between; }
.meter { block-size: 4px; overflow: hidden; border-radius: 999px; background: var(--well); }
.meter span { display: block; block-size: 100%; border-radius: inherit; background: var(--ink); transition: inline-size 240ms cubic-bezier(.2, .8, .2, 1); }
.batch-items { display: grid; margin: 0; padding: 0; list-style: none; }
.batch-items li { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 2px 16px; align-items: baseline; padding: 8px 0; border-top: 1px solid var(--rule); }
.batch-items p { grid-column: 1 / -1; margin: 0; }
.batch-items .result { font-weight: 500; }
.batch-items .failed .result { font-weight: 700; }
.batch-items .skipped .result { color: var(--ink-2); }
dialog ul { max-height: 240px; padding-left: 20px; overflow: auto; }
@media (max-width: 720px) {
  .maintenance { bottom: 8px; }
  .dock { padding: 8px 10px 10px 12px; }
  .tools { display: grid; grid-template-columns: minmax(0, 1fr) auto; }
  .clear { grid-row: 1; grid-column: 2; margin-left: 0; }
  .tool-actions { display: grid; grid-column: 1 / -1; grid-template-columns: 1fr 1fr; }
  .tool-actions button { justify-content: flex-start; }
  .tools .hint { grid-column: 1 / -1; font-size: 12px; }
}
@media (prefers-reduced-motion: reduce) { .meter span { transition: none; } }
</style>
