import { expect, it } from 'vitest'
import { driveBatch, type Batch, type Invoke } from '../src/batch'
const initial: Batch={ref:'test',mode:'refresh',state:'running',sequence:0,total:2,processed:0,started_at_ms:0,results:[]}
it('uses acknowledged sequence and resumes the same request after a lost response',async()=>{
 let saved={...initial},calls:number[]=[],failed=false,mutations=0
 const invoke:Invoke=async<T>(_action:string,payload:Record<string,unknown>={})=>{const sequence=Number(payload?.sequence);calls.push(sequence);if(sequence===saved.sequence){mutations++;saved={...saved,sequence:sequence+1,processed:sequence+1,state:sequence===1?'completed':'running'}};if(!failed){failed=true;throw Error('lost response')}return {batch:saved} as T}
 await expect(driveBatch(invoke,initial,new AbortController().signal,()=>{},async()=>{})).rejects.toThrow('lost response')
 const result=await driveBatch(invoke,initial,new AbortController().signal,()=>{},async()=>{})
 expect(calls).toEqual([0,0,1]);expect(mutations).toBe(2);expect(result.state).toBe('completed')
})
it('stops remaining accounts on abort without undoing the completed item',async()=>{
 const controller=new AbortController(),calls:string[]=[]
 const invoke:Invoke=async<T>(action:string)=>{calls.push(action);if(action==='accounts.batch.step'){controller.abort();return {batch:{...initial,sequence:1,processed:1}} as T}return {batch:{...initial,sequence:1,processed:1,state:'canceled'}} as T}
 const result=await driveBatch(invoke,initial,controller.signal,()=>{},async()=>{})
 expect(calls).toEqual(['accounts.batch.step','accounts.batch.cancel']);expect(result.processed).toBe(1);expect(result.state).toBe('canceled')
})
it('an already aborted newly created batch never advances an account',async()=>{
 const controller=new AbortController();controller.abort();const calls:string[]=[]
 const invoke:Invoke=async<T>(action:string)=>{calls.push(action);return {batch:{...initial,state:'canceled'}} as T}
 await driveBatch(invoke,initial,controller.signal,()=>{},async()=>{})
 expect(calls).toEqual(['accounts.batch.cancel'])
})
