import { webcrypto } from 'node:crypto'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { encryptPassword, subjectFor } from '../src/model'

afterEach(() => vi.unstubAllGlobals())

describe('key input transport', () => {
  it('uses the process public key and context label without sending the passphrase', async () => {
    vi.stubGlobal('crypto', webcrypto)
    const pair = await crypto.subtle.generateKey({ name: 'RSA-OAEP', modulusLength: 2048, publicExponent: new Uint8Array([1, 0, 1]), hash: 'SHA-256' }, true, ['encrypt', 'decrypt'])
    const jwk = await crypto.subtle.exportKey('jwk', pair.publicKey)
    const marker = 'synthetic-passphrase-for-test'
    const sealed = await encryptPassword(marker, jwk)
    expect(sealed).not.toContain(marker)
    const bytes = Uint8Array.from(atob(sealed), char => char.charCodeAt(0))
    const plain = await crypto.subtle.decrypt({ name: 'RSA-OAEP', label: new TextEncoder().encode('raylea.mihoyo-accounts/key-input-v1') }, pair.privateKey, bytes)
    expect(new TextDecoder().decode(plain)).toBe(marker)
    await expect(crypto.subtle.decrypt({ name: 'RSA-OAEP' }, pair.privateKey, bytes)).rejects.toThrow()
  })

  it('refuses plaintext fallback when the browser cannot encrypt', async () => {
    vi.stubGlobal('crypto', {})
    await expect(encryptPassword('synthetic-passphrase', {})).rejects.toThrow('HTTPS')
  })
})

it('keeps adapter and bot scope when binding the same user ID', () => {
  const first = subjectFor({ source_protocol: 'onebot11', source_adapter: 'first', id: 'bot-a' }, 'user')
  const second = subjectFor({ source_protocol: 'onebot11', source_adapter: 'second', id: 'bot-b' }, 'user')
  expect(first).not.toEqual(second)
  expect(first.actor_id).toBe(second.actor_id)
})
