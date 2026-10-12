<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref } from 'vue'
import { usePluginHost } from '@rayleabot/plugin-ui'
import KeyProtection from './KeyProtection.vue'
import ArkToken from './ArkToken.vue'
import AccountMaintenance from './AccountMaintenance.vue'
import AccountStatistics from './AccountStatistics.vue'
import CloudAuthorizations from './CloudAuthorizations.vue'
import DeviceSettings from './DeviceSettings.vue'
import AccountAssociations from './AccountAssociations.vue'
import CookieAuthorization from './CookieAuthorization.vue'
import AccountCard from './AccountCard.vue'
import Avatar from './Avatar.vue'
import Icon from './Icon.vue'
import { isQQ, personOf, resolveNicknames, type Person } from './identity'
import { encryptPassword, errorText, finished, flagOf, gameNames, loginLabel, realmName, shortDate, vaultNames, type Account, type Login, type Status, type Subject } from './model'

type Tab = 'all' | 'attention' | 'valid'
type TransitionDocument = { startViewTransition?: (update: () => Promise<void>) => { ready: Promise<void>; finished: Promise<void> } }

const host = usePluginHost()
const status = ref<Status | null>(null)
const accounts = ref<Account[]>([])
const marked = ref<string[]>([])
const markedAccounts = computed(() => accounts.value.filter(a => marked.value.includes(a.ref)))
const total = ref(0)
const page = ref(0)
const nextPage = ref<number | null>(null)
const busy = ref(false)
const error = ref('')
const notice = ref('')
const search = ref('')
const tab = ref<Tab>('all')
const adding = ref<'qr' | 'cookie' | null>(null)
const actorID = ref('')
const login = ref<Login | null>(null)
const password = ref('')
const protection = ref('dpapi')
const selected = ref<string | null>(null)
const transitionRef = ref<string | null>(null)
const callers = ref<string[]>([])
const removal = ref<Account | null>(null)
const removeDialog = ref<HTMLDialogElement | null>(null)
const maintenance = ref<InstanceType<typeof AccountMaintenance> | null>(null)
const deskElement = ref<HTMLElement | null>(null)
const nicknames = ref<ReadonlyMap<string, string>>(new Map())
const identityState = ref<'idle' | 'loading' | 'ready' | 'offline' | 'failed'>('idle')
const resolvedIDs = new Set<string>()
let pollTimer: ReturnType<typeof setTimeout> | undefined
let disposed = false
let maintenanceChanged = false
function maintenanceUpdated() { maintenanceChanged = true }
function maintenanceWorking(value: boolean) { busy.value = value; if (!value && maintenanceChanged) { maintenanceChanged = false; void run(load) } }

const unlocked = computed(() => status.value?.vault_state === 'unlocked')
const bot = computed(() => host.bot.value ? { source_protocol: host.bot.value.source_protocol, id: host.bot.value.bot_id } : null)
const loginActive = computed(() => login.value && !finished(login.value))
const needsPassword = computed(() => status.value?.vault_state === 'uninitialized' ? protection.value === 'password' : status.value?.key_protection === 'password')
const selectedAccount = computed(() => accounts.value.find(account => account.ref === selected.value))
const deskOpen = computed(() => adding.value !== null || selectedAccount.value !== undefined)
const ownerLabel = computed(() => bot.value?.source_protocol === 'onebot11' ? '账号主人的 QQ 号' : '账号主人的聊天用户 ID')

function person(owner: Subject): Person { return personOf(owner, nicknames.value) }
function describe(account: Account) { return `${person(account.owner).name} · ${realmName(account)} ${account.account_label}` }
const draftPerson = computed(() => {
  const owner = bot.value && { source_protocol: bot.value.source_protocol, bot_id: bot.value.id, actor_id: actorID.value.trim() }
  return owner && isQQ(owner) ? personOf(owner, nicknames.value) : null
})

// The login keeps the owner it was started for, even if the input changes later.
const loginOwner = ref('')
const loginPerson = computed(() => loginOwner.value && bot.value ? personOf({ source_protocol: bot.value.source_protocol, bot_id: bot.value.id, actor_id: loginOwner.value }, nicknames.value) : null)

// Cards of one owner sit together; owners keep the order they were first bound in.
const ordered = computed(() => {
  const first = new Map<string, number>()
  accounts.value.forEach((account, index) => { if (!first.has(account.owner.actor_id)) first.set(account.owner.actor_id, index) })
  return accounts.value.map((account, index) => ({ account, index }))
    .sort((a, b) => first.get(a.account.owner.actor_id)! - first.get(b.account.owner.actor_id)! || a.index - b.index)
    .map(item => item.account)
})
const owned = computed(() => accounts.value.reduce((counts, account) => counts.set(account.owner.actor_id, (counts.get(account.owner.actor_id) ?? 0) + 1), new Map<string, number>()))
const flagged = computed(() => accounts.value.filter(account => flagOf(account)))
const tabs = computed(() => [
  { id: 'all' as const, label: '全部', count: accounts.value.length },
  { id: 'attention' as const, label: '需处理', count: flagged.value.length },
  { id: 'valid' as const, label: '可用', count: accounts.value.length - flagged.value.length },
])
const visible = computed(() => {
  const query = search.value.trim().toLowerCase()
  return ordered.value.filter(account => {
    if (tab.value === 'attention' && !flagOf(account)) return false
    if (tab.value === 'valid' && flagOf(account)) return false
    return !query || [account.account_label, account.owner.actor_id, nicknames.value.get(account.owner.actor_id) ?? '', ...account.roles.flatMap(role => [role.uid, role.nickname])].some(value => value.toLowerCase().includes(query))
  })
})
const allMarked = computed(() => visible.value.length > 0 && visible.value.every(account => marked.value.includes(account.ref)))
const someMarked = computed(() => !allMarked.value && visible.value.some(account => marked.value.includes(account.ref)))
function markAll(value: boolean) {
  const refs = new Set(visible.value.map(account => account.ref))
  marked.value = value ? [...new Set([...marked.value, ...refs])] : marked.value.filter(ref => !refs.has(ref))
}
function mark(account: Account, value: boolean) {
  marked.value = value ? [...marked.value, account.ref] : marked.value.filter(ref => ref !== account.ref)
}

// The band names the one thing to do next, most urgent first.
const band = computed(() => {
  if (!accounts.value.length) return null
  const invalid = accounts.value.filter(account => account.status === 'invalid')
  const verify = flagged.value.filter(account => account.status !== 'invalid' && ['device_required', 'challenge_required'].includes(account.last_check?.state ?? ''))
  const unknown = flagged.value.filter(account => account.status !== 'invalid' && !verify.includes(account))
  if (invalid.length) return { tone: 'urgent', count: invalid.length, text: '个账号需要重新登录：请账号主人重新扫码，或清理失效账号。', action: 'attention' as const }
  if (verify.length) return { tone: 'urgent', count: verify.length, text: '个账号需要在官方应用完成验证。', action: 'attention' as const }
  if (unknown.length) return { tone: 'urgent', count: unknown.length, text: '个账号还没有确认登录状态。', action: 'check' as const, refs: unknown.map(account => account.ref) }
  return { tone: 'calm', count: accounts.value.length, text: '个账号全部可用。', action: null }
})
async function checkUnknown() {
  if (band.value?.action !== 'check' || !band.value.refs) return
  marked.value = [...band.value.refs]
  await nextTick()
  await maintenance.value?.start('check')
}
const identityNote = computed(() => {
  if (!accounts.value.length) return ''
  if (!accounts.value.some(account => isQQ(account.owner))) return 'QQ 官方机器人的用户只有 openid，没有昵称与头像。'
  if (identityState.value === 'loading') return '正在读取 QQ 昵称…'
  if (identityState.value === 'offline') return '机器人离线，暂时只显示 QQ 号；上线后刷新列表即可显示昵称。'
  if (identityState.value === 'failed') return 'QQ 昵称读取失败，暂时只显示 QQ 号。'
  return ''
})

async function invoke<T>(action: string, payload: Record<string, unknown> = {}): Promise<T> {
  return await host.client.invokeAction(action, payload) as unknown as T
}
async function reloadAccounts() {
  const result = await invoke<{ items: Account[]; total: number; next_page?: number }>('accounts.list', { page: page.value })
  accounts.value = result.items
  marked.value = marked.value.filter(ref => accounts.value.some(a => a.ref === ref))
  total.value = result.total
  nextPage.value = result.next_page ?? null
  void resolveOwners()
}
// Nicknames are display-only: they come from the bot's live connection when it
// is online, and every card falls back to the QQ number.
async function resolveOwners() {
  const ids = [...new Set(accounts.value.filter(account => isQQ(account.owner)).map(account => account.owner.actor_id))].filter(id => !resolvedIDs.has(id))
  if (!ids.length || identityState.value === 'loading') return
  identityState.value = 'loading'
  try {
    const names = await resolveNicknames(host, ids)
    if (disposed) return
    if (!names) { identityState.value = 'offline'; return }
    ids.forEach(id => resolvedIDs.add(id))
    nicknames.value = new Map([...nicknames.value, ...names])
    identityState.value = 'ready'
  } catch {
    if (!disposed) identityState.value = 'failed'
  }
}
async function load() {
  status.value = await invoke<Status>('status')
  if (!status.value.native_key_available) protection.value = 'password'
  if (unlocked.value) await reloadAccounts()
  else accounts.value = []
}
async function run(work: () => Promise<void>, success = '') {
  if (busy.value) return
  busy.value = true
  error.value = ''; notice.value = ''
  try { await work(); notice.value = success } catch (cause) { error.value = errorText(cause) }
  finally { busy.value = false }
}
void host.ready.then(() => run(load)).catch(cause => { error.value = errorText(cause) })

async function openVault() {
  await run(async () => {
    const payload: Record<string, unknown> = {}
    if (needsPassword.value) {
      const { public_key } = await invoke<{ public_key: JsonWebKey }>('vault.public_key')
      payload.password_ciphertext = await encryptPassword(password.value, public_key)
    }
    password.value = ''
    await invoke(status.value?.vault_state === 'uninitialized' ? 'vault.initialize' : 'vault.unlock', payload)
    await load()
  }, '账号库已解锁')
  password.value = ''
}
async function lockVault() {
  await run(async () => {
    await invoke('vault.lock')
    clearTimeout(pollTimer); login.value = null; adding.value = null; selected.value = null
    await load()
  })
}
// Below the two-column width the desk sits above the sheet, so bring it into view.
async function revealDesk() {
  await nextTick()
  if (typeof matchMedia === 'function' && matchMedia('(max-width: 1180px)').matches) deskElement.value?.scrollIntoView({ block: 'start' })
}
function openAdd(mode: 'qr' | 'cookie') {
  const opening = adding.value === null
  selected.value = null
  adding.value = mode
  if (mode === 'qr' && !loginActive.value) login.value = null
  if (opening) void revealDesk()
}
async function closeAdd() {
  if (loginActive.value) await cancelLogin()
  if (!loginActive.value) adding.value = null
}
async function startLogin() {
  if (!bot.value || !actorID.value.trim()) return
  await run(async () => {
    const response = await invoke<{ login: Login }>('login.start', { actor_id: actorID.value.trim() })
    loginOwner.value = actorID.value.trim()
    login.value = response.login
    schedulePoll()
  })
}
function schedulePoll() {
  clearTimeout(pollTimer)
  if (!disposed && loginActive.value) pollTimer = setTimeout(() => { void pollLogin() }, 2000)
}
async function pollLogin() {
  const ref = login.value?.ref
  if (!ref || disposed) return
  try {
    const response = await invoke<{ login: Login }>('login.poll', { login_ref: ref })
    if (login.value?.ref !== ref || disposed) return
    login.value = response.login
    if (response.login.state === 'bound') await reloadAccounts()
    schedulePoll()
  } catch (cause) { if (!disposed) error.value = errorText(cause) }
}
async function cancelLogin() {
  await run(async () => {
    clearTimeout(pollTimer)
    if (loginActive.value) {
      const response = await invoke<{ login: Login }>('login.cancel', { login_ref: login.value!.ref })
      login.value = response.login
    }
  })
}
async function updateAccount(account: Account, action: string, extra: Record<string, unknown> = {}) {
  await run(async () => {
    try { await invoke(action, { account_ref: account.ref, ...extra }) } finally { await reloadAccounts() }
    if (selected.value === account.ref) callers.value = [...(selectedAccount.value?.allowed_callers ?? [])]
  }, '账号已更新')
}

// Pulling a card moves it from its sleeve to the desk; the view transition
// carries the same card between the two places.
async function swap(change: () => void, ref: string | null) {
  const doc = document as TransitionDocument
  const reduced = typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches
  if (!ref || !doc.startViewTransition || reduced) { change(); return }
  transitionRef.value = ref
  await nextTick()
  const transition = doc.startViewTransition(async () => { change(); await nextTick() })
  // A skipped transition (for example in a hidden tab) still applies the change.
  transition.ready.catch(() => {})
  await transition.finished.catch(() => {})
  transitionRef.value = null
}
async function pull(account: Account) {
  await swap(() => { adding.value = null; selected.value = account.ref; callers.value = [...account.allowed_callers] }, account.ref)
  await revealDesk()
}
function putBack() { void swap(() => { selected.value = null }, selected.value) }

async function askRemove(account: Account) {
  removal.value = account
  await nextTick()
  removeDialog.value?.showModal()
}
async function removeAccount() {
  if (!removal.value) return
  await run(async () => {
    await invoke('accounts.remove', { account_ref: removal.value!.ref })
    await reloadAccounts()
    removeDialog.value?.close()
    selected.value = null; removal.value = null
  }, '账号已移除')
}
async function changePage(value: number) { await run(async () => { page.value = value; selected.value = null; await reloadAccounts() }) }
onUnmounted(() => { disposed = true; clearTimeout(pollTimer); password.value = '' })
</script>

<template>
  <main :class="{ closed: status && !unlocked }" :aria-busy="busy || host.loading.value">
    <div v-if="error" class="feedback danger" role="alert">
      <Icon name="alert" />
      <span>{{ error }}</span>
      <button type="button" class="quiet" :disabled="busy" @click="loginActive ? pollLogin() : run(load)">重试</button>
    </div>
    <p v-if="notice" class="feedback" role="status"><Icon name="check" />{{ notice }}</p>

    <div v-if="!status" class="loading" role="status">
      <span class="sr-only">正在读取账号状态…</span>
      <span v-for="n in 6" :key="n" class="skeleton" aria-hidden="true" />
    </div>

    <template v-else>
      <div v-if="!unlocked" class="cover-slot">
        <span class="cover-tab">{{ status.vault_state === 'uninitialized' ? '未初始化' : status.vault_state === 'missing' || status.vault_error ? '缺少密钥' : '已锁定' }}</span>
        <section class="vault-panel" aria-labelledby="vault-heading">
        <h2 id="vault-heading">{{ status.vault_state === 'missing' || status.vault_error ? '恢复原密钥后继续' : status.vault_state === 'uninitialized' ? '设置账号库' : '解锁后使用账号' }}</h2>
        <p class="vault-state">{{ status.vault_error ? '需要恢复账号库密钥' : vaultNames[status.vault_state] }} · 一次登录，供已授权的游戏插件使用。</p>
        <p v-if="status.vault_error" class="danger-text" role="alert">{{ status.vault_error.message }}</p>
        <p v-if="status.vault_state === 'missing'">检测到已保存账号，但缺少对应密钥。请恢复本插件数据目录中的原密钥文件，再重新加载插件。</p>
        <form v-else @submit.prevent="openVault">
          <p v-if="status.vault_state === 'uninitialized'">账号将在保存前加密。请选择密钥保护方式，并将密钥文件与数据库一同备份。</p>
          <label v-if="status.vault_state === 'uninitialized' && status.native_key_available">密钥保护方式
            <select v-model="protection" :disabled="busy"><option value="dpapi">当前 Windows 用户</option><option value="password">恢复口令</option></select>
          </label>
          <p v-if="!needsPassword && !status.vault_error" class="hint">使用当前 Windows 用户解锁。此方式依赖原系统用户环境；需要换机或更强的隔离时，可在解锁后改用恢复口令。</p>
          <p v-if="status.vault_error" class="hint">复制原环境更新后的 master-key.json 后，刷新状态，再输入恢复口令解锁。现有账号数据会保留。</p>
          <label v-if="needsPassword">恢复口令
            <input v-model="password" type="password" :disabled="busy" :autocomplete="status.vault_state === 'uninitialized' ? 'new-password' : 'current-password'" required aria-describedby="password-help">
            <span id="password-help" class="hint">至少 12 字节。请妥善保存；丢失口令后无法恢复账号。</span>
          </label>
          <div class="actions">
            <button class="primary" :disabled="busy" type="submit">{{ busy ? '正在处理…' : status.vault_state === 'uninitialized' ? '初始化账号库' : '解锁账号库' }}</button>
            <button v-if="status.vault_error" :disabled="busy" type="button" @click="run(load)">刷新状态</button>
          </div>
        </form>
        </section>
      </div>

      <template v-else>
        <div v-if="band" class="band" :class="band.tone">
          <p>
            <span v-if="band.tone === 'calm'" class="band-mark" aria-hidden="true"><Icon name="check" :size="16" /></span>
            <strong class="band-count num">{{ band.count }}</strong> {{ band.text }}
          </p>
          <button v-if="band.action === 'attention' && tab !== 'attention'" type="button" class="quiet" @click="tab = 'attention'">只看需处理<Icon name="chevron" :size="16" /></button>
          <button v-else-if="band.action === 'check'" type="button" class="quiet" :disabled="busy" @click="checkUnknown"><Icon name="shield" :size="16" />检查这些账号</button>
        </div>

        <section class="binder" aria-labelledby="accounts-heading">
          <h2 id="accounts-heading" class="sr-only">已绑定账号</h2>
          <div class="binder-head">
            <label v-if="visible.length" class="select-all">
              <input type="checkbox" :checked="allMarked" :indeterminate="someMarked" :disabled="busy" :aria-label="`全选当前显示的 ${visible.length} 个账号`" @change="markAll(($event.target as HTMLInputElement).checked)">
              <span aria-hidden="true">全选</span>
            </label>
            <div v-if="accounts.length" class="tabs" role="group" aria-label="按状态筛选">
              <button v-for="item in tabs" :key="item.id" type="button" class="tab" :aria-pressed="tab === item.id" @click="tab = item.id">{{ item.label }}<span class="count">{{ item.count }}</span></button>
            </div>
            <label v-if="accounts.length" class="search">
              <Icon name="search" :size="16" />
              <span class="sr-only">搜索昵称、QQ、通行证或 UID</span>
              <input v-model="search" type="search" placeholder="搜索昵称、QQ、通行证或 UID">
            </label>
            <div class="head-actions">
              <button type="button" class="icon-button" :disabled="busy" aria-label="刷新列表" title="刷新列表" @click="run(load)"><Icon name="refresh" /></button>
              <button v-if="bot" type="button" :disabled="busy" @click="openAdd('cookie')"><Icon name="import" />导入 Cookie</button>
              <button v-if="accounts.length" type="button" class="primary" :disabled="busy" @click="openAdd('qr')"><Icon name="qr" />扫码添加账号</button>
            </div>
          </div>
          <p v-if="identityNote" class="identity-note">{{ identityNote }}</p>

          <div class="binder-body" :class="{ open: deskOpen }">
            <div class="sheet-area">
              <div v-if="!accounts.length" class="empty">
                <span class="empty-sleeves" aria-hidden="true"><span /><span /><span /></span>
                <h3>还没有绑定账号</h3>
                <p>扫码后自动发现原神、星穹铁道和绝区零角色。群友也可以在聊天中向机器人发送“扫码登录”自行绑定。</p>
                <button class="primary" type="button" :disabled="busy" @click="openAdd('qr')"><Icon name="qr" />扫码添加账号</button>
              </div>
              <div v-else-if="!visible.length" class="empty">
                <h3>没有符合条件的账号</h3>
                <p>当前页没有匹配的名片，试试其他昵称、QQ 号或 UID。</p>
                <button type="button" @click="search = ''; tab = 'all'">显示全部账号</button>
              </div>
              <ul v-else class="sheet">
                <li v-for="account in visible" :key="account.ref" class="slot">
                  <div v-if="selected === account.ref" class="sleeve">
                    <span>{{ person(account.owner).name }} 的名片已取出</span>
                    <button type="button" class="quiet" @click="putBack">放回</button>
                  </div>
                  <AccountCard v-else :account="account" :person="person(account.owner)" :owned="owned.get(account.owner.actor_id)" :busy="busy" :marked="marked.includes(account.ref)" :transition="transitionRef === account.ref" @mark="mark(account, $event)" @check="updateAccount(account, 'accounts.check')" @refresh="updateAccount(account, 'accounts.refresh')" @pull="pull(account)" />
                </li>
                <li v-if="tab === 'all' && !search && nextPage === null" class="slot">
                  <button type="button" class="sleeve add" :disabled="busy" @click="openAdd('qr')"><Icon name="plus" :size="20" />扫码添加账号</button>
                </li>
              </ul>
              <nav v-if="page > 0 || nextPage !== null" class="pagination" aria-label="账号分页">
                <button type="button" :disabled="busy || page === 0" @click="changePage(page - 1)">上一页</button>
                <span>第 {{ page + 1 }} 页 · 共 {{ total }} 个账号</span>
                <button type="button" :disabled="busy || nextPage === null" @click="changePage(nextPage!)">下一页</button>
              </nav>
              <AccountMaintenance ref="maintenance" :selected="markedAccounts" :disabled="busy" :invoke="invoke" :describe="describe" @changed="maintenanceUpdated" @working="maintenanceWorking" @clear="marked = []" />
            </div>

            <aside v-if="deskOpen" ref="deskElement" class="desk">
              <section v-if="adding" class="desk-panel" aria-labelledby="add-heading">
                <div class="desk-head">
                  <h2 id="add-heading">添加账号</h2>
                  <button type="button" class="icon-button" :disabled="busy" :aria-label="loginActive ? '取消登录并关闭' : '关闭添加账号'" @click="closeAdd"><Icon name="close" /></button>
                </div>
                <div v-if="bot" class="segmented" role="group" aria-label="添加方式">
                  <button type="button" :aria-pressed="adding === 'qr'" :disabled="busy" @click="openAdd('qr')"><Icon name="qr" :size="16" />米游社扫码</button>
                  <button type="button" :aria-pressed="adding === 'cookie'" :disabled="busy || !!loginActive" @click="openAdd('cookie')"><Icon name="import" :size="16" />导入 Cookie</button>
                </div>
                <template v-if="adding === 'qr'">
                  <form v-if="!loginActive && login?.state !== 'bound'" class="login-form" @submit.prevent="startLogin">
                    <label>{{ ownerLabel }}
                      <span class="owner-input">
                        <Avatar v-if="draftPerson" :person="draftPerson" :size="36" />
                        <input v-model="actorID" :disabled="busy" required autocomplete="off" spellcheck="false" placeholder="输入要绑定账号的聊天用户 ID">
                      </span>
                    </label>
                    <button class="primary" type="submit" :disabled="busy || !bot || !actorID.trim()">{{ busy ? '正在获取二维码…' : '获取米游社二维码' }}</button>
                  </form>
                  <p v-if="login && loginPerson" class="binding-target">
                    <Avatar :person="loginPerson" :size="32" />
                    <span>为 <strong>{{ loginPerson.name }}</strong><template v-if="!loginPerson.name.startsWith(`${loginPerson.idLabel} `)">（{{ loginPerson.idLabel }} {{ loginPerson.id }}）</template> 绑定米游社账号</span>
                  </p>
                  <div v-if="login" class="login-result">
                    <img v-if="login.qr_code && loginActive" :src="login.qr_code" width="220" height="220" alt="使用米游社扫描此登录二维码">
                    <div class="login-state" aria-live="polite">
                      <strong>{{ loginLabel(login) }}</strong>
                      <p v-if="loginActive" class="hint">二维码和登录结果仅用于当前绑定任务。</p>
                      <p v-if="login.error_message" :class="login.state === 'failed' ? 'danger-text' : 'attention'">{{ login.error_message }}{{ loginActive ? ' 正在退避重试。' : '' }}</p>
                      <p v-if="login.failure?.retcode || login.failure?.http_status" class="hint">{{ login.failure.operation }} · {{ login.failure.retcode ? `米游社错误码 ${login.failure.retcode}` : `HTTP ${login.failure.http_status}` }}</p>
                      <ul v-if="Object.keys(login.role_states).length" class="discovery">
                        <li v-for="(state, game) in login.role_states" :key="game"><span>{{ gameNames[game] || game }}</span>{{ state === 'ready' ? '已发现角色' : state === 'empty' ? '没有绑定角色' : '暂未读取，可稍后重试' }}</li>
                      </ul>
                      <div class="actions">
                        <button v-if="loginActive" type="button" :disabled="busy" @click="cancelLogin">取消登录</button>
                        <button v-else-if="login.state === 'bound'" type="button" :disabled="busy" @click="login = null">继续添加</button>
                      </div>
                    </div>
                  </div>
                </template>
                <CookieAuthorization v-else-if="bot" :busy="busy" :owner-label="ownerLabel" :invoke="invoke" @changed="maintenanceUpdated" @working="maintenanceWorking" />
              </section>

              <section v-else-if="selectedAccount" class="desk-panel" aria-labelledby="desk-heading">
                <div class="desk-head">
                  <button type="button" class="quiet" @click="putBack"><Icon name="back" :size="16" />放回名片册</button>
                  <h2 id="desk-heading" class="sr-only">管理 {{ person(selectedAccount.owner).name }} 的账号</h2>
                </div>
                <div class="desk-card">
                  <AccountCard large :account="selectedAccount" :person="person(selectedAccount.owner)" :owned="owned.get(selectedAccount.owner.actor_id)" :busy="busy" :transition="transitionRef === selectedAccount.ref" @check="updateAccount(selectedAccount, 'accounts.check')" @refresh="updateAccount(selectedAccount, 'accounts.refresh')" />
                </div>

                <div class="reverse">
                <section class="desk-section" aria-labelledby="roles-heading">
                  <h3 id="roles-heading">游戏角色</h3>
                  <table v-if="selectedAccount.roles.length" class="roles">
                    <thead><tr><th scope="col">游戏</th><th scope="col">角色</th><th scope="col">UID</th><th scope="col">区服</th><th scope="col">等级</th></tr></thead>
                    <tbody><tr v-for="role in selectedAccount.roles" :key="role.ref"><td>{{ gameNames[role.game] || role.game }}</td><td>{{ role.nickname }}</td><td class="num">{{ role.uid }}</td><td class="code">{{ role.region }}</td><td class="num">{{ role.level }}</td></tr></tbody>
                  </table>
                  <p v-else class="hint">尚未发现角色，可按游戏重新读取。</p>
                  <div class="actions">
                    <button v-for="(label, game) in gameNames" :key="game" type="button" :disabled="busy" @click="updateAccount(selectedAccount, 'accounts.roles', { game })">重新读取{{ label }}</button>
                  </div>
                </section>

                <section class="desk-section" aria-labelledby="public-heading">
                  <h3 id="public-heading">公共查询池</h3>
                  <template v-if="selectedAccount.public_query?.enabled">
                    <p>本人已开启：{{ selectedAccount.public_query.games.map(game => gameNames[game] || game).join('、') }} · 每日 <span class="num">{{ selectedAccount.public_query.daily_limit }}</span> 次 · {{ selectedAccount.public_query.day || '尚未使用' }} 已用 <span class="num">{{ selectedAccount.public_query.used }}</span> 次。</p>
                    <div class="actions"><button type="button" :disabled="busy" @click="updateAccount(selectedAccount, 'accounts.public.disable')">退出公共查询池</button></div>
                  </template>
                  <p v-else class="hint">未开启。需要共享时，由账号所属用户本人发送“共享查询账号 开启”设置游戏范围与额度。</p>
                </section>

                <DeviceSettings :key="`device-${selectedAccount.ref}`" :account="selectedAccount" :busy="busy" :invoke="invoke" @changed="maintenanceUpdated" @working="maintenanceWorking" />
                <CloudAuthorizations v-if="selectedAccount.realm !== 'os'" :key="`cloud-${selectedAccount.ref}`" :account="selectedAccount" :busy="busy" :invoke="invoke" @changed="maintenanceUpdated" @working="maintenanceWorking" />

                <form class="desk-section" @submit.prevent="updateAccount(selectedAccount, 'accounts.permissions', { callers, revision: selectedAccount.revision })">
                  <fieldset :disabled="busy">
                    <legend>允许使用此账号的插件</legend>
                    <div class="checks">
                      <label v-for="entry in status.allowed_plugins" :key="entry.plugin_id"><input v-model="callers" type="checkbox" :value="entry.plugin_id"><span>{{ entry.games.map(game => gameNames[game] || game).join('、') }}<small>{{ entry.plugin_id }}</small></span></label>
                    </div>
                  </fieldset>
                  <div class="actions"><button type="submit" :disabled="busy" class="primary">保存授权</button></div>
                </form>

                <div class="desk-danger">
                  <button type="button" :disabled="busy" @click="askRemove(selectedAccount)"><Icon name="trash" :size="16" />移除账号</button>
                  <span class="hint">最近更新 {{ shortDate(selectedAccount.updated_at_ms) }}</span>
                </div>
                </div>
              </section>
            </aside>
          </div>
        </section>

        <section class="inside" aria-labelledby="inside-heading">
          <h2 id="inside-heading">账号库与设置</h2>
          <div class="inside-grid">
            <div class="setting">
              <section aria-labelledby="vault-state-heading">
                <h3 id="vault-state-heading">账号库</h3>
                <p>{{ vaultNames[status.vault_state] }}</p>
                <p class="hint">一次登录，供已授权的游戏插件使用。锁定后，游戏插件暂时无法使用这些账号。</p>
                <div class="actions"><button type="button" :disabled="busy" @click="lockVault"><Icon name="lock" :size="16" />锁定账号库</button></div>
              </section>
              <AccountStatistics :invoke="invoke" :disabled="busy || !accounts.length" />
            </div>
            <div class="setting">
              <KeyProtection :invoke="invoke" :protection="status.key_protection" :native-available="status.native_key_available" :busy="busy" @changed="maintenanceUpdated" @working="maintenanceWorking" />
              <ArkToken :invoke="invoke" :busy="busy" />
            </div>
            <AccountAssociations class="setting" :bot="bot" :person="person" :invoke="invoke" :disabled="busy" />
          </div>
        </section>
      </template>
    </template>

    <footer class="page-foot"><a href="./source.zip" download>本版本对应源码</a></footer>
    <dialog ref="removeDialog" aria-labelledby="remove-title" aria-describedby="remove-description" @cancel="busy && $event.preventDefault()">
      <h2 id="remove-title">移除 {{ removal ? person(removal.owner).name : '' }} 的{{ removal ? realmName(removal) : '' }}账号</h2>
      <p id="remove-description">通行证 <span class="num">{{ removal?.account_label }}</span> 将从账号库移除，游戏插件无法继续使用此账号。之后可以重新扫码绑定。</p>
      <p v-if="error" class="danger-text" role="alert">{{ error }}</p>
      <div class="actions">
        <button autofocus type="button" :disabled="busy" @click="removeDialog?.close()">取消</button>
        <button type="button" class="destructive" :disabled="busy" @click="removeAccount">{{ busy ? '正在移除…' : '移除账号' }}</button>
      </div>
    </dialog>
  </main>
</template>
