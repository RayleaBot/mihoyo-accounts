export type Invoke = <T>(action: string, payload?: Record<string, unknown>) => Promise<T>
const maxBytes = 16 * 1024 * 1024
export function canonicalBackup(text: string): string {
  if (text.length > maxBytes) throw new Error('备份文件超过 16 MiB。')
  let value: Record<string, unknown>
  try { value = JSON.parse(text) } catch { throw new Error('请选择加密账号备份 JSON。') }
  if (!value || value.format !== 'raylea-accounts-encrypted' || value.version !== 1 || value.iterations !== 600000 || typeof value.salt !== 'string' || typeof value.sealed !== 'string' || !/^[A-Za-z0-9+/]{43}=$/.test(value.salt) || !/^[A-Za-z0-9+/]+={0,2}$/.test(value.sealed) || value.sealed.length < 40) throw new Error('文件不是受支持的加密账号备份。')
  return JSON.stringify({ format: value.format, version: value.version, iterations: value.iterations, salt: value.salt, sealed: value.sealed })
}
export async function downloadBackup(invoke: Invoke, passwordCiphertext: string): Promise<string> {
  const { transfer } = await invoke<{ transfer: { ref: string; size: number } }>('vault.export.start', { password_ciphertext: passwordCiphertext })
  try {
    if (!Number.isInteger(transfer.size) || transfer.size <= 0 || transfer.size > maxBytes) throw new Error('备份大小无效。')
    let offset = 0, content = ''
    while (offset < transfer.size) {
      const part = await invoke<{ chunk: string; next_offset: number; more: boolean }>('vault.export.read', { ref: transfer.ref, offset })
      if (typeof part.chunk !== 'string' || part.chunk.length > 65536 || part.next_offset !== offset + part.chunk.length || part.next_offset <= offset || part.next_offset > transfer.size || part.more !== (part.next_offset < transfer.size)) throw new Error('备份传输未完整推进，请重新导出。')
      content += part.chunk; offset = part.next_offset
    }
    return canonicalBackup(content)
  } finally { await invoke('vault.transfer.cancel', { ref: transfer.ref }).catch(() => {}) }
}
export async function uploadBackup(invoke: Invoke, text: string, passwordCiphertext: string): Promise<number> {
  const data = canonicalBackup(text)
  const { transfer } = await invoke<{ transfer: { ref: string } }>('vault.import.start')
  let finished = false
  try {
    for (let offset = 0; offset < data.length; offset += 65536) {
      const chunk = data.slice(offset, offset + 65536)
      const response = await invoke<{ offset: number }>('vault.import.append', { ref: transfer.ref, offset, chunk })
      if (response.offset !== offset + chunk.length) throw new Error('备份上传进度不一致，请重新导入。')
    }
    const result = await invoke<{ restored: number }>('vault.import.finish', { ref: transfer.ref, password_ciphertext: passwordCiphertext })
    finished = true
    return result.restored
  } finally { if (!finished) await invoke('vault.transfer.cancel', { ref: transfer.ref }).catch(() => {}) }
}
