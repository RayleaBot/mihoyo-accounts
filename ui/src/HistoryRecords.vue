<script setup lang="ts">
import { ref } from 'vue'
import type { BotAccount, PluginUIClient } from '@rayleabot/plugin-ui'
import { errorText, gameNames, type Account, type Subject } from './model'
import { assignmentBots, subjectState, type HistoryState } from './history'
interface Link { ref: string; primary: Subject; secondary: Subject }
interface SubjectData { key: string; owner?: Subject; uid_bindings: Record<string, { uids: string[]; current?: string }>; default_roles: Record<string, { account_ref: string; role_ref: string }> }
interface Delegation { ref: string; account_ref: string; owner: Subject; operation: string; task_id: string }
interface Entry { key: string; kind: 'account' | 'association' | 'binding' | 'delegation'; state: HistoryState; owners: Subject[]; account?: Account; link?: Link; data?: SubjectData; delegation?: Delegation }
const props = defineProps<{ client: PluginUIClient; disabled: boolean }>()
const emit = defineEmits<{ changed: [] }>()
const entries = ref<Entry[]>([]), bots = ref<BotAccount[]>([]), target = ref(''), busy = ref(false), error = ref(''), loaded = ref(false), confirmation = ref<Entry | null>(null), assigning = ref<Subject | null>(null)
const names = { pending: '机器人账号待确认', outside: '机器人已不在目录', unknown: '归属资料缺失', present: '目录内账号' }
function label(owner: Subject) { return `${owner.source_protocol} / ${owner.bot_id || '待确认'} / 用户 ${owner.actor_id}` }
async function invoke<T>(action: string, payload: Record<string, unknown> = {}) { return await props.client.invokeAction(action, payload) as T }
async function read() {
  const all: Account[] = []
  const grants: Delegation[] = []
  let page: number | undefined = 0
  while (page !== undefined) {
    const result: { items: Account[]; next_page?: number } = await invoke('maintenance.list', { page })
    grants.push(...(await invoke<{ items: Delegation[] }>('history.delegations.list', { page })).items)
    all.push(...result.items); page = result.next_page ?? undefined
  }
  const links = (await invoke<{ items: Link[] }>('history.associations.list')).items
  const keys = [...new Set(Object.keys((await props.client.reloadSecretStatus()).configured).filter(k => /^subject\.[a-f0-9]{64}\.(uids|defaults)$/.test(k)).map(k => k.slice(0, k.lastIndexOf('.'))))]
  const data: SubjectData[] = []
  for (let i = 0; i < keys.length; i += 128) data.push(...(await invoke<{ items: SubjectData[] }>('history.subjects.list', { keys: keys.slice(i, i + 128) })).items)
  const states = new Map<string, HistoryState>()
  async function state(owner?: Subject) {
    if (!owner) return 'unknown' as const
    const key = JSON.stringify([owner.source_protocol, owner.bot_id])
    if (!states.has(key)) states.set(key, await subjectState(props.client, owner))
    return states.get(key)!
  }
  const next: Entry[] = []
  for (const account of all) {
    const status = await state(account.owner)
    if (status !== 'present') next.push({ key: account.ref, kind: 'account', state: status, owners: [account.owner], account })
  }
  for (const link of links) {
    const statuses = await Promise.all([state(link.primary), state(link.secondary)])
    if (statuses.some(s => s !== 'present')) next.push({ key: link.ref, kind: 'association', state: statuses.includes('pending') ? 'pending' : 'outside', owners: [link.primary, link.secondary], link })
  }
  for (const item of data) {
    const status = await state(item.owner)
    if (status !== 'present') next.push({ key: item.key, kind: 'binding', state: status, owners: item.owner ? [item.owner] : [], data: item })
  }
  for (const grant of grants) {
    const status = await state(grant.owner)
    if (status !== 'present') next.push({ key: grant.ref, kind: 'delegation', state: status, owners: [grant.owner], delegation: grant })
  }
  entries.value = next; loaded.value = true
}
async function run(work: () => Promise<void>) {
  if (busy.value || props.disabled) return
  busy.value = true; error.value = ''
  try { await work() } catch (cause) { error.value = errorText(cause) } finally { busy.value = false }
}
async function remove() {
  const entry = confirmation.value!
  await run(async () => {
    if (entry.kind === 'account') await invoke('history.remove', { account_ref: entry.key })
    else if (entry.kind === 'association') await invoke('history.associations.remove', { ref: entry.key, confirm: true })
    else if (entry.kind === 'delegation') await invoke('history.delegations.remove', { account_ref: entry.delegation!.account_ref, ref: entry.key, confirm: true })
    else await invoke('history.subjects.remove', { key: entry.key, confirm: true })
    confirmation.value = null; await read(); emit('changed')
  })
}
async function choose(owner: Subject) {
  await run(async () => { bots.value = await assignmentBots(props.client); target.value = ''; assigning.value = owner })
}
async function assign() {
  await run(async () => {
    // The host validates this target from the directory and supplies ManagementBot.
    // The business payload contains only the unresolved old subject.
    await props.client.apiRequest('POST', `/api/plugins/${encodeURIComponent(props.client.pluginId)}/management/actions`, {
      page_id: 'accounts', bot: { source_protocol: 'onebot11', bot_id: target.value },
      action: 'history.assign', payload: { legacy_subject: assigning.value, confirm: true },
    })
    assigning.value = null; await read(); emit('changed')
  })
}
</script>
<template>
  <section>
    <div class="section-heading"><h2>历史数据与归属 <span v-if="loaded" class="count">{{ entries.length }}</span></h2><button :disabled="busy || disabled" @click="run(read)">读取历史记录</button></div>
    <p class="hint">列出缺少机器人账号、目录外账号及归属资料缺失的记录。仅缺少账号的 OneBot11 身份可归入目录账号；QQ 官方身份和完整账号不能改挂。</p>
    <p v-if="error" role="alert" class="danger-text">{{ error }}</p>
    <p v-if="loaded && !entries.length" class="empty">没有需要处理的历史记录。</p>
    <ul class="account-list">
      <li v-for="entry in entries" :key="entry.kind + entry.key" class="account-detail">
        <h3>{{ names[entry.state] }} · {{ entry.kind === 'account' ? `米游社 / HoYoLab ${entry.account!.account_label}` : entry.kind === 'association' ? '聊天关联' : entry.kind === 'delegation' ? '定时委托' : '默认角色与 UID 绑定' }}</h3>
        <div v-for="owner in entry.owners" :key="JSON.stringify(owner)">
          <p>{{ label(owner) }}</p>
          <button v-if="owner.source_protocol === 'onebot11' && !owner.bot_id" :disabled="busy || disabled" @click="choose(owner)">归入机器人账号</button>
        </div>
        <p v-if="!entry.owners.length" class="hint">旧记录只保留主体摘要，无法还原身份；可查看并删除。</p>
        <p v-for="role in entry.account?.roles" :key="role.ref">{{ gameNames[role.game] }} · {{ role.nickname }} · {{ role.uid }}</p>
        <p v-if="entry.delegation">{{ entry.delegation.operation }} · {{ entry.delegation.task_id }}</p>
        <template v-if="entry.data">
          <p v-for="(binding, game) in entry.data.uid_bindings" :key="game">{{ gameNames[game] }}：{{ binding.uids.join('、') }} · 当前 {{ binding.current || '未选择' }}</p>
          <p v-for="(selected, game) in entry.data.default_roles" :key="game">{{ gameNames[game] }}默认角色：{{ selected.role_ref }}</p>
        </template>
        <div class="actions"><button :disabled="busy || disabled" class="danger-text" @click="confirmation = entry">删除此记录</button></div>
        <div v-if="confirmation?.key === entry.key" class="actions"><span>确认删除此记录？</span><button :disabled="busy || disabled" class="destructive" @click="remove">确认删除</button><button :disabled="busy" @click="confirmation = null">取消</button></div>
      </li>
    </ul>
    <form v-if="assigning" class="account-detail" @submit.prevent="assign">
      <p>将 {{ label(assigning) }} 的账号、关联与绑定归入所选机器人。</p>
      <label>OneBot11 账号<select v-model="target" required :disabled="busy"><option disabled value="">请选择账号</option><option v-for="bot in bots" :key="bot.bot_id" :value="bot.bot_id">{{ bot.nickname || bot.bot_id }} · {{ bot.bot_id }}</option></select></label>
      <p v-if="!bots.length" class="hint">目录中没有 OneBot11 账号。</p>
      <div class="actions"><button :disabled="busy || disabled || !target">确认归属</button><button type="button" :disabled="busy" @click="assigning = null">取消</button></div>
    </form>
  </section>
</template>
