import { describe, expect, it } from 'vitest'
import { canonicalBackup, downloadBackup, uploadBackup, type Invoke } from '../src/migration'
const envelope = { format: 'raylea-accounts-encrypted', version: 1, iterations: 600000, salt: 'A'.repeat(43) + '=', sealed: 'B'.repeat(100) }
describe('encrypted account transfer', () => {
  it('rejects plaintext and strips unrelated fields before transport', () => {
    expect(() => canonicalBackup('{"cookie_token":"synthetic-plain"}')).toThrow()
    expect(JSON.parse(canonicalBackup(JSON.stringify({ ...envelope, token: 'synthetic-not-uploaded' })))).toEqual(envelope)
    expect(() => canonicalBackup(JSON.stringify({ ...envelope, iterations: 1 }))).toThrow()
  })
  it('downloads only complete advancing encrypted content and closes the task', async () => {
    const data = JSON.stringify(envelope), calls: string[] = []
    const invoke: Invoke = async <T>(action: string) => { calls.push(action); return (action === 'vault.export.start' ? { transfer: { ref: 'synthetic', size: data.length } } : action === 'vault.export.read' ? { chunk: data, next_offset: data.length, more: false } : {}) as T }
    expect(await downloadBackup(invoke, 'synthetic-rsa-ciphertext')).toBe(data)
    expect(calls.at(-1)).toBe('vault.transfer.cancel')
  })
  it('never includes the original file metadata or a plaintext password in upload actions', async () => {
    const calls: { action: string; payload: Record<string, unknown> }[] = []
    const invoke: Invoke = async <T>(action: string, payload: Record<string, unknown> = {}) => { calls.push({ action, payload }); return (action === 'vault.import.start' ? { transfer: { ref: 'synthetic' } } : action === 'vault.import.append' ? { offset: String(payload.chunk).length } : { restored: 1 }) as T }
    expect(await uploadBackup(invoke, JSON.stringify({ ...envelope, cookie: 'synthetic-do-not-forward' }), 'synthetic-rsa-ciphertext')).toBe(1)
    expect(JSON.stringify(calls)).not.toContain('synthetic-do-not-forward'); expect(calls.some(c => 'password' in c.payload)).toBe(false)
  })
})
