<script setup lang="ts">
import { onUnmounted, ref } from 'vue'
import Icon from './Icon.vue'
import { driveBatch, type Batch, type Invoke } from './batch'
import { errorText, gameNames, shortDate } from './model'
const props = defineProps<{ disabled: boolean; invoke: Invoke }>()
const busy = ref(false), error = ref(''), batch = ref<Batch | null>(null)
let controller: AbortController | undefined, disposed = false
// Statistics only read saved summaries, so the run does not lock the page.
async function run() {
  if (busy.value || props.disabled) return
  busy.value = true; error.value = ''; controller = new AbortController()
  try {
    if (batch.value?.state === 'running') await props.invoke('accounts.batch.cancel', { ref: batch.value.ref }).catch(() => {})
    batch.value = (await props.invoke<{ batch: Batch }>('accounts.batch.start', { mode: 'statistics', items: [], confirm: true })).batch
    const current = await driveBatch(props.invoke, batch.value, controller.signal, value => { if (!disposed) batch.value = value })
    if (!disposed) batch.value = current
  } catch (cause) { if (!disposed) error.value = errorText(cause) }
  finally { controller = undefined; busy.value = false }
}
onUnmounted(() => { disposed = true; controller?.abort() })
</script>

<template>
  <section class="statistics-panel" aria-labelledby="statistics-heading">
    <h3 id="statistics-heading">账号统计</h3>
    <p class="hint">分页读取已保存的账号摘要，不访问米游社，也不刷新凭据。</p>
    <div class="actions"><button type="button" :disabled="disabled || busy" @click="run"><Icon name="chart" :size="16" />{{ busy ? '正在统计…' : batch ? '重新统计' : '统计已绑定账号' }}</button></div>
    <p v-if="error" role="alert" class="danger-text">{{ error }}</p>
    <template v-if="batch?.statistics">
      <dl class="statistics" aria-live="polite">
        <div><dt>本轮读取账号</dt><dd>{{ batch.statistics.accounts }}</dd></div>
        <div><dt>聊天主体（含机器人归属）</dt><dd>{{ batch.statistics.subjects }}</dd></div>
        <div><dt>保存状态可用</dt><dd>{{ batch.statistics.states.valid || 0 }}</dd></div>
        <div><dt>保存状态失效</dt><dd>{{ batch.statistics.states.invalid || 0 }}</dd></div>
        <div><dt>待检查</dt><dd>{{ batch.statistics.states.unknown || 0 }}</dd></div>
        <div><dt>具备刷新材料</dt><dd>{{ batch.statistics.refreshable }}</dd></div>
        <div><dt>主动开启公共查询</dt><dd>{{ batch.statistics.public_queries }}</dd></div>
        <div v-for="(name, game) in gameNames" :key="game"><dt>{{ name }}保存角色</dt><dd>{{ batch.statistics.game_roles[game] || 0 }}</dd></div>
      </dl>
      <p class="hint">读取了 {{ batch.statistics.pages }} 页保存摘要<template v-if="batch.finished_at_ms">，{{ shortDate(batch.finished_at_ms) }} 完成</template>；分页期间账号可能变化，此结果不是同一时刻快照。</p>
    </template>
  </section>
</template>

<style scoped>
.statistics-panel { display: grid; gap: 12px; min-width: 0; }
.statistics { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 0 20px; margin: 0; }
.statistics > div { display: flex; justify-content: space-between; gap: 12px; padding: 8px 0; border-top: 1px solid var(--line); }
.statistics dt { color: var(--ink-2); }
.statistics dd { margin: 0; font-weight: 650; font-variant-numeric: tabular-nums; }
</style>
