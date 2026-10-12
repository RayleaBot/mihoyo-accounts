<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref } from 'vue'
import { usePluginHost } from '@rayleabot/plugin-ui'
import KeyProtection from './KeyProtection.vue'
import ArkToken from './ArkToken.vue'
import AccountMaintenance from './AccountMaintenance.vue'
import CloudAuthorizations from './CloudAuthorizations.vue'
import DeviceSettings from './DeviceSettings.vue'
import AccountAssociations from './AccountAssociations.vue'
import CookieAuthorization from './CookieAuthorization.vue'
import { botAccounts, botLabel, ownerBotLabel, accountNames, checkNames, encryptPassword, errorText, finished, gameNames, loginLabel, subjectFor, vaultNames, type Account, type Login, type Status } from './model'

const host = usePluginHost()
const status = ref<Status | null>(null)
const accounts = ref<Account[]>([])
const marked=ref<string[]>([])
const markedAccounts=computed(()=>accounts.value.filter(a=>marked.value.includes(a.ref)))
const total = ref(0)
const page = ref(0)
const nextPage = ref<number | null>(null)
const busy = ref(false)
const error = ref('')
const notice = ref('')
const search = ref('')
const showLogin = ref(false)
const botIndex = ref(0)
const actorID = ref('')
const login = ref<Login | null>(null)
const password = ref('')
const protection = ref('dpapi')
const selected = ref<string | null>(null)
const callers = ref<string[]>([])
const removal = ref<Account | null>(null)
const removeDialog = ref<HTMLDialogElement | null>(null)
let pollTimer: ReturnType<typeof setTimeout> | undefined
let disposed = false
let maintenanceChanged = false
function maintenanceUpdated() { maintenanceChanged = true }
function maintenanceWorking(value: boolean) { busy.value = value; if (!value && maintenanceChanged) { maintenanceChanged = false; void run(load) } }

const unlocked = computed(() => status.value?.vault_state === 'unlocked')
const visible = computed(() => accounts.value.filter(account => {
  const query = search.value.trim().toLowerCase()
  return !query || [account.account_label, account.owner.actor_id, ...account.roles.flatMap(role => [role.uid, role.nickname])].some(value => value.toLowerCase().includes(query))
}))
const bots = computed(() => botAccounts(status.value?.bots ?? []))
const bot = computed(() => bots.value[botIndex.value])
const loginActive = computed(() => login.value && !finished(login.value))
const needsPassword = computed(() => status.value?.vault_state === 'uninitialized' ? protection.value === 'password' : status.value?.key_protection === 'password')
const selectedAccount = computed(() => accounts.value.find(account => account.ref === selected.value))

async function invoke<T>(action: string, payload: Record<string, unknown> = {}): Promise<T> {
  return await host.client.invokeAction(action, payload) as unknown as T
}
async function reloadAccounts() {
  const result = await invoke<{ items: Account[]; total: number; next_page?: number }>('accounts.list', { page: page.value })
  accounts.value = result.items
  marked.value=marked.value.filter(ref=>accounts.value.some(a=>a.ref===ref))
  total.value = result.total
  nextPage.value = result.next_page ?? null
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
    clearTimeout(pollTimer); login.value = null; showLogin.value = false; selected.value = null
    await load()
  })
}
async function startLogin() {
  if (!bot.value || !actorID.value.trim()) return
  await run(async () => {
    const response = await invoke<{ login: Login }>('login.start', { subject: subjectFor(bot.value!, actorID.value) })
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
    showLogin.value = false
  })
}
async function updateAccount(account: Account, action: string, extra: Record<string, unknown> = {}) {
  await run(async () => {
    try {await invoke(action, { account_ref: account.ref, ...extra })} finally {await reloadAccounts()}
    if (selected.value === account.ref) callers.value = [...(selectedAccount.value?.allowed_callers ?? [])]
  }, '账号已更新')
}
function expand(account: Account) {
  selected.value = selected.value === account.ref ? null : account.ref
  callers.value = [...account.allowed_callers]
}
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
function dateText(value: number) { return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(value) }
onUnmounted(() => { disposed = true; clearTimeout(pollTimer); password.value = '' })
</script>

<template>
  <main :aria-busy="busy || host.loading.value">
    <div v-if="error" class="feedback danger" role="alert">
      <span>{{ error }}</span>
      <button type="button" :disabled="busy" @click="loginActive ? pollLogin() : run(load)">重试</button>
    </div>
    <p v-if="notice" class="feedback" role="status">{{ notice }}</p>
    <p v-if="!status" class="empty" role="status">正在读取账号状态…</p>

    <template v-else>
      <div class="toolbar">
        <div><strong>{{ status.vault_error ? '需要恢复账号库密钥' : vaultNames[status.vault_state] }}</strong><p>一次登录，供已授权的游戏插件使用。</p></div>
        <div class="actions">
          <button v-if="unlocked" type="button" :disabled="busy" @click="lockVault">锁定账号库</button>
          <button v-if="unlocked && !showLogin" type="button" class="primary" :disabled="busy" @click="showLogin = true; login = null">扫码添加账号</button>
        </div>
      </div>

      <section v-if="!unlocked" class="vault-panel" aria-labelledby="vault-heading">
        <h2 id="vault-heading">{{ status.vault_state === 'missing' || status.vault_error ? '恢复原密钥后继续' : status.vault_state === 'uninitialized' ? '设置账号库' : '解锁后使用账号' }}</h2>
        <p v-if="status.vault_error" role="alert">{{ status.vault_error.message }}</p>
        <p v-if="status.vault_state === 'missing'">检测到已保存账号，但缺少对应密钥。请恢复本插件数据目录中的原密钥文件，再重新加载插件。</p>
        <form v-else @submit.prevent="openVault">
          <p v-if="status.vault_state === 'uninitialized'">账号将在保存前加密。请选择密钥保护方式，并将密钥文件与数据库一同备份。</p>
          <label v-if="status.vault_state === 'uninitialized' && status.native_key_available">密钥保护方式
            <select v-model="protection" :disabled="busy"><option value="dpapi">当前 Windows 用户</option><option value="password">恢复口令</option></select>
          </label>
          <p v-if="!needsPassword && !status.vault_error" class="muted">使用当前 Windows 用户解锁。此方式依赖原系统用户环境；需要换机或更强的隔离时，可在解锁后改用恢复口令。</p>
          <p v-if="status.vault_error" class="muted">复制原环境更新后的 master-key.json 后，刷新状态，再输入恢复口令解锁。现有账号数据会保留。</p>
          <label v-if="needsPassword">恢复口令
            <input v-model="password" type="password" :disabled="busy" :autocomplete="status.vault_state === 'uninitialized' ? 'new-password' : 'current-password'" required aria-describedby="password-help">
            <span id="password-help" class="hint">至少 12 字节。请妥善保存；丢失口令后无法恢复账号。</span>
          </label>
          <button class="primary" :disabled="busy" type="submit">{{ busy ? '正在处理…' : status.vault_state === 'uninitialized' ? '初始化账号库' : '解锁账号库' }}</button>
          <button v-if="status.vault_error" :disabled="busy" type="button" @click="run(load)">刷新状态</button>
        </form>
      </section>

      <template v-else>
        <section v-if="showLogin" class="login-panel" aria-labelledby="login-heading">
          <div class="section-heading"><h2 id="login-heading">扫码添加账号</h2><button type="button" :disabled="busy" @click="cancelLogin">{{ loginActive ? '取消登录' : '收起' }}</button></div>
          <form v-if="!loginActive && login?.state !== 'bound'" class="login-form" @submit.prevent="startLogin">
            <label>绑定到机器人账号
              <select v-model="botIndex" :disabled="busy || !bots.length" required>
                <option v-for="(entry, index) in bots" :key="`${entry.source_protocol}:${entry.id}`" :value="index">{{ botLabel(entry) }} · {{ entry.source_protocol }}</option>
              </select>
            </label>
            <label>聊天用户 ID<input v-model="actorID" :disabled="busy" required autocomplete="off" placeholder="输入要绑定账号的聊天用户 ID"></label>
            <p v-if="!bots.length" class="hint">没有在线机器人。连接机器人后刷新页面，或让用户向机器人发送“扫码登录”。</p>
            <button class="primary" type="submit" :disabled="busy || !bot || !actorID.trim()">{{ busy ? '正在获取二维码…' : '获取米游社二维码' }}</button>
          </form>
          <div v-if="login" class="login-result">
            <img v-if="login.qr_code && loginActive" :src="login.qr_code" width="256" height="256" alt="使用米游社扫描此登录二维码">
            <div aria-live="polite">
              <strong>{{ loginLabel(login) }}</strong>
              <p v-if="loginActive" class="muted">二维码和登录结果仅用于当前绑定任务。</p>
              <p v-if="login.error_message" :class="login.state === 'failed' ? 'danger-text' : 'attention'">{{ login.error_message }}{{ loginActive ? ' 正在退避重试。' : '' }}</p>
              <p v-if="login.failure?.retcode || login.failure?.http_status" class="hint">{{ login.failure.operation }} · {{ login.failure.retcode ? `米游社错误码 ${login.failure.retcode}` : `HTTP ${login.failure.http_status}` }}</p>
              <ul v-if="Object.keys(login.role_states).length" class="discovery">
                <li v-for="(state, game) in login.role_states" :key="game">{{ gameNames[game] || game }}：{{ state === 'ready' ? '已发现角色' : state === 'empty' ? '没有绑定角色' : '暂未读取，可稍后重试' }}</li>
              </ul>
            </div>
          </div>
        </section>

        <section aria-labelledby="accounts-heading">
          <div class="section-heading"><h2 id="accounts-heading">已绑定账号 <span class="count">{{ total }}</span></h2><button :disabled="busy" type="button" @click="run(load)">刷新列表</button></div>
          <AccountMaintenance :bots="bots" :selected="markedAccounts" :disabled="busy" :invoke="invoke" @changed="maintenanceUpdated" @working="maintenanceWorking" />
          <label v-if="accounts.length" class="search"><span class="sr-only">搜索账号、用户或 UID</span><input v-model="search" type="search" placeholder="搜索账号、用户或 UID"></label>
          <div v-if="!accounts.length" class="empty"><h3>还没有绑定账号</h3><p>扫码后自动发现原神、星穹铁道和绝区零角色。</p><button class="primary" type="button" :disabled="busy" @click="showLogin = true">扫码添加账号</button></div>
          <p v-else-if="!visible.length" class="empty">当前页没有匹配账号，试试其他用户 ID 或 UID。</p>
          <ul v-else class="account-list">
            <li v-for="account in visible" :key="account.ref">
              <div class="account-row">
                <label class="account-selector"><span class="sr-only">选择此账号</span><input v-model="marked" type="checkbox" :value="account.ref" :disabled="busy" :aria-label="`选择账号 ${account.account_label} ${account.owner.actor_id}`"></label>
                <div class="account-info"><div class="identity"><strong>{{ account.realm==='os'?'HoYoLab':'米游社' }} {{ account.account_label }}</strong><span class="status" :class="{ attention: account.status === 'invalid' || account.subject_pending }">{{ account.subject_pending ? '机器人账号待确认' : accountNames[account.status] || '待检查' }}</span></div><p>{{ account.owner.actor_id }} <span class="muted">· {{ ownerBotLabel(account.owner, bots) }}</span></p><p class="games">{{ [...new Set(account.roles.map(role => gameNames[role.game] || role.game))].join(' · ') || '尚未发现游戏角色' }}</p></div>
                <div class="actions"><button :disabled="busy" type="button" @click="updateAccount(account, 'accounts.check')">检查状态</button><button :disabled="busy || !account.can_refresh" type="button" @click="updateAccount(account, 'accounts.refresh')">刷新凭据</button><button :disabled="busy" type="button" :aria-expanded="selected === account.ref" @click="expand(account)">管理</button></div>
              </div>
              <p v-if="account.last_check" class="hint">最近{{ account.last_check.kind === 'refresh' ? '刷新' : '检查' }}：{{ checkNames[account.last_check.state] || '暂未确认' }} · {{ new Date(account.last_check.checked_at_ms).toLocaleString('zh-CN') }}</p>
              <div v-if="selected === account.ref" class="account-detail">
                <h3>游戏角色</h3>
                <p v-if="account.public_query?.enabled" class="hint">本人已开启公共资料查询：{{ account.public_query.games.map(game => gameNames[game] || game).join('、') }} · 每日 {{ account.public_query.daily_limit }} 次 · {{ account.public_query.day || '尚未使用' }} 已用 {{ account.public_query.used }} 次。<button type="button" :disabled="busy" @click="updateAccount(account, 'accounts.public.disable')">退出公共查询池</button></p>
                <p v-else class="hint">公共查询未开启。需要共享时，由账号所属用户本人发送“共享查询账号 开启”设置游戏范围与额度。</p>
                <ul v-if="account.roles.length" class="roles"><li v-for="role in account.roles" :key="role.ref"><span>{{ gameNames[role.game] }}</span><strong>{{ role.nickname }}</strong><span class="mono">{{ role.uid }}</span><span class="muted">等级 {{ role.level }}</span></li></ul>
                <p v-else class="muted">尚未发现角色，可按游戏重新读取。</p>
                <div class="actions"><button v-for="(label, game) in gameNames" :key="game" type="button" :disabled="busy" @click="updateAccount(account, 'accounts.roles', { game })">重新读取{{ label }}</button></div>
                <DeviceSettings :account="account" :busy="busy" :invoke="invoke" @changed="maintenanceUpdated" @working="maintenanceWorking" />
                <CloudAuthorizations v-if="account.realm!=='os'" :account="account" :busy="busy" :invoke="invoke" @changed="maintenanceUpdated" @working="maintenanceWorking" />
                <form @submit.prevent="updateAccount(account, 'accounts.permissions', { callers, revision: account.revision })">
                  <fieldset :disabled="busy"><legend>允许使用此账号的插件</legend><div class="checks"><label v-for="entry in status.allowed_plugins" :key="entry.plugin_id"><input v-model="callers" type="checkbox" :value="entry.plugin_id"><span>{{ entry.games.map(game => gameNames[game] || game).join('、') }} <small>{{ entry.plugin_id }}</small></span></label></div></fieldset>
                  <div class="detail-footer"><button type="submit" :disabled="busy" class="primary">保存授权</button><button type="button" class="danger-text" :disabled="busy" @click="askRemove(account)">移除账号</button><span class="hint">最近更新 {{ dateText(account.updated_at_ms) }}</span></div>
                </form>
              </div>
            </li>
          </ul>
          <div v-if="page > 0 || nextPage !== null" class="pagination"><button :disabled="busy || page === 0" @click="changePage(page - 1)">上一页</button><span>第 {{ page + 1 }} 页</span><button :disabled="busy || nextPage === null" @click="changePage(nextPage!)">下一页</button></div>
        </section>
        <CookieAuthorization :bots="bots" :busy="busy" :invoke="invoke" @changed="maintenanceUpdated" @working="maintenanceWorking"/>
        <AccountAssociations :bots="bots" :invoke="invoke" :disabled="busy"/>
        <KeyProtection :invoke="invoke" :protection="status.key_protection" :native-available="status.native_key_available" :busy="busy" @changed="maintenanceUpdated" @working="maintenanceWorking" />
        <ArkToken :invoke="invoke" :busy="busy" />
      </template>
    </template>

    <footer class="hint"><a href="./source.zip" download>本版本对应源码</a></footer>
    <dialog ref="removeDialog" aria-labelledby="remove-title" aria-describedby="remove-description" @cancel="busy && $event.preventDefault()">
      <h2 id="remove-title">移除账号 {{ removal?.account_label }}</h2><p id="remove-description">游戏插件将无法继续使用此账号。之后可以重新扫码绑定。</p><p v-if="error" class="danger-text" role="alert">{{ error }}</p><div class="actions"><button autofocus type="button" :disabled="busy" @click="removeDialog?.close()">取消</button><button type="button" class="destructive" :disabled="busy" @click="removeAccount">{{ busy ? '正在移除…' : '移除账号' }}</button></div>
    </dialog>
  </main>
</template>
