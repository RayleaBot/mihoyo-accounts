export interface Batch {
  ref: string; mode: 'check' | 'refresh' | 'remove_invalid' | 'statistics'
  state: 'running' | 'completed' | 'canceled'; sequence: number; total: number; processed: number
  started_at_ms: number; finished_at_ms?: number
  results: { account_ref: string; label: string; state: 'success' | 'failed' | 'skipped'; code?: string; message?: string }[]
  statistics?: { accounts: number; subjects: number; states: Record<string, number>; game_roles: Record<string, number>; refreshable: number; public_queries: number; pages: number }
}
export type Invoke = <T>(action: string, payload?: Record<string, unknown>) => Promise<T>
function pause(signal: AbortSignal) {
  return new Promise<void>(resolve => {
    const done = () => { clearTimeout(timer); signal.removeEventListener('abort', done); resolve() }
    const timer = setTimeout(done, 500); signal.addEventListener('abort', done, { once: true }); if (signal.aborted) done()
  })
}
export async function driveBatch(invoke: Invoke, initial: Batch, signal: AbortSignal, progress: (batch: Batch) => void, wait = pause): Promise<Batch> {
  let current = initial
  while (current.state === 'running') {
    if (signal.aborted) { current = (await invoke<{ batch: Batch }>('accounts.batch.cancel', { ref: current.ref })).batch; progress(current); break }
    current = (await invoke<{ batch: Batch }>('accounts.batch.step', { ref: current.ref, sequence: current.sequence })).batch
    progress(current)
    if (current.state === 'running') await wait(signal)
  }
  return current
}
