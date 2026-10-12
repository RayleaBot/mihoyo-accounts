export interface Subject { source_protocol: string; bot_id: string; actor_id: string }
export interface Bot { source_protocol: string; source_adapter?: string; id: string; nickname?: string }
export interface Role { ref: string; game: string; uid: string; region: string; nickname: string; level: number }
export interface Account { subject_pending?: boolean; realm?: 'cn'|'os'; shared?:boolean; cloud_configured?:string[]; ref: string; revision: number; owner: Subject; account_label: string; roles: Role[]; allowed_callers: string[]; status: string; can_refresh: boolean; updated_at_ms: number; last_check?: {kind:'check'|'refresh';state:string;code?:string;checked_at_ms:number}; public_query?: { enabled: boolean; games: string[]; daily_limit: number; day: string; used: number } }
export const checkNames: Record<string,string> = {available:'通过',auth_invalid:'登录失效',device_required:'需要设备验证',challenge_required:'需要官方验证',unavailable:'暂未确认'}
export interface Status { vault_state: 'uninitialized' | 'locked' | 'unlocked' | 'missing'; vault_error?: { code: string; message: string; details?: { reason?: string } }; native_key_available: boolean; key_protection: string; allowed_plugins: { plugin_id: string; games: string[] }[]; bots: Bot[] }
export interface Login { ref: string; state: string; phase: string; expires_at_ms: number; qr_code?: string; account?: Account; role_states: Record<string, string>; error_code?: string; error_message?: string; failure?: { stage: string; operation?: string; retcode?: number; http_status?: number; reason?: string } }

export const gameNames: Record<string, string> = { genshin: '原神', starrail: '星穹铁道', zzz: '绝区零' }
export const vaultNames: Record<string, string> = { unlocked: '账号库已解锁', locked: '账号库已锁定', uninitialized: '首次使用', missing: '需要恢复密钥' }
export const accountNames: Record<string, string> = { valid: '可用', invalid: '需重新登录', unknown: '待检查' }
export function finished(login: Login): boolean { return ['bound', 'expired', 'canceled', 'failed'].includes(login.state) }
export function loginLabel(login: Login): string {
  if (login.state === 'bound') return '登录成功，账号已保存'
  if (login.state === 'expired') return '二维码已过期，请重新扫码'
  if (login.state === 'canceled') return '登录已取消'
  if (login.state === 'failed') return '登录未完成，请重试'
  if (login.phase === 'exchange') return '已确认，正在建立账号连接'
  if (login.phase === 'roles') return '正在发现游戏角色'
  if (login.phase === 'saving') return '正在保存账号'
  if (login.state === 'scanned') return '已扫描，请在米游社确认'
  return '打开米游社，扫码并确认登录'
}
export function subjectFor(bot: Bot, actor: string): Subject { return { source_protocol: bot.source_protocol, bot_id: bot.id, actor_id: actor.trim() } }
export function botAccounts(bots: Bot[]): Bot[] {
  const accounts = new Map<string, Bot>()
  for (const bot of bots) {
    if (!bot.id) continue
    const key = `${bot.source_protocol}:${bot.id}`
    if (!accounts.has(key) || !accounts.get(key)?.nickname && bot.nickname) accounts.set(key, bot)
  }
  return [...accounts.values()]
}
export function botLabel(bot: Bot): string { return bot.nickname ? `${bot.nickname}（${bot.id}）` : bot.id }
export function ownerBotLabel(owner: Subject, bots: Bot[]): string {
  if (!owner.bot_id) return '待选择机器人账号'
  const bot = bots.find(bot => bot.source_protocol === owner.source_protocol && bot.id === owner.bot_id)
  return bot ? botLabel(bot) : owner.bot_id
}
export function errorText(error: unknown): string { return error instanceof Error ? error.message : '操作未完成，请重试。' }

export async function encryptPassword(password: string, jwk: JsonWebKey): Promise<string> {
  if (!globalThis.crypto?.subtle) throw new Error('当前连接无法加密口令，请使用 HTTPS 或本机地址打开管理页。')
  const bytes = new TextEncoder().encode(password)
  if (bytes.length < 12 || bytes.length > 128) throw new Error('恢复口令需要 12–128 字节，请使用较长的字母、数字组合。')
  try {
    const key = await crypto.subtle.importKey('jwk', jwk, { name: 'RSA-OAEP', hash: 'SHA-256' }, false, ['encrypt'])
    const sealed = await crypto.subtle.encrypt({ name: 'RSA-OAEP', label: new TextEncoder().encode('raylea.mihoyo-accounts/key-input-v1') }, key, bytes)
    return btoa(String.fromCharCode(...new Uint8Array(sealed)))
  } finally { bytes.fill(0) }
}
