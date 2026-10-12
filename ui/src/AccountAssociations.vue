<script setup lang="ts">
import { ref } from 'vue'
import Avatar from './Avatar.vue'
import Icon from './Icon.vue'
import type { Person } from './identity'
import { shortDate, type Subject } from './model'
interface Link { ref: string; primary: Subject; secondary: Subject; created_ms: number }
const props = defineProps<{ bot: { source_protocol: string; id: string } | null; person: (subject: Subject) => Person; invoke: <T>(action: string, payload?: Record<string, unknown>) => Promise<T>; disabled: boolean }>()
const items = ref<Link[]>([]), busy = ref(false), error = ref(''), candidate = ref(''), loaded = ref(false)
// A linked identity may live on another bot account; only that side names its bot.
function detail(subject: Subject, role: string) {
  const person = props.person(subject)
  const parts = [role]
  if (!person.name.startsWith(`${person.idLabel} `)) parts.push(`${person.idLabel} ${person.id}`)
  if (subject.source_protocol !== props.bot?.source_protocol || subject.bot_id !== props.bot?.id) parts.push(`其他机器人账号 ${subject.bot_id}`)
  return parts.join(' · ')
}
async function load() { if (busy.value || props.disabled) return; busy.value = true; error.value = ''; try { items.value = (await props.invoke<{ items: Link[] }>('accounts.associations.list')).items; loaded.value = true } catch (e) { error.value = e instanceof Error ? e.message : '关联列表读取失败。' } finally { busy.value = false } }
async function remove() { busy.value = true; error.value = ''; try { await props.invoke('accounts.associations.remove', { ref: candidate.value, confirm: true }); items.value = items.value.filter(v => v.ref !== candidate.value); candidate.value = '' } catch (e) { error.value = e instanceof Error ? e.message : '解除关联未完成。' } finally { busy.value = false } }
</script>

<template>
  <section class="associations" aria-labelledby="associations-heading">
    <h3 id="associations-heading">本人跨聊天关联</h3>
    <p class="hint">在已登录的主聊天身份发送“关联账号”，另一身份发送“申请关联 编号”，最后回到主聊天核对目标并完成确认。关联共用账号记录，CK 不复制给其他插件；双方原有账号保留。</p>
    <p class="hint">解除关联后，附属身份的后续查询和定时委托停止访问共享账号。恢复账号备份后需重新关联。</p>
    <div class="actions"><button type="button" :disabled="disabled || busy" @click="load"><Icon name="link" :size="16" />{{ loaded ? '重新读取关联' : '读取关联列表' }}</button></div>
    <p v-if="error" role="alert" class="danger-text">{{ error }}</p>
    <p v-if="loaded && !items.length" class="hint">没有已建立的关联。</p>
    <ul v-if="items.length" class="links">
      <li v-for="link in items" :key="link.ref">
        <div class="pair">
          <div v-for="side in [{ subject: link.primary, role: '主身份' }, { subject: link.secondary, role: '附属身份' }]" :key="side.role" class="side">
            <Avatar :person="person(side.subject)" :size="32" />
            <div><strong>{{ person(side.subject).name }}</strong><small>{{ detail(side.subject, side.role) }}</small></div>
          </div>
        </div>
        <div class="link-foot">
          <span class="hint">{{ shortDate(link.created_ms) }} 建立</span>
          <button v-if="candidate !== link.ref" type="button" class="quiet" :disabled="busy || disabled" @click="candidate = link.ref"><Icon name="close" :size="16" />解除此关联</button>
        </div>
        <div v-if="candidate === link.ref" class="confirm-strip">
          <p>确认停止上述身份之间的账号共享？</p>
          <div class="actions"><button type="button" :disabled="busy" @click="candidate = ''">取消</button><button type="button" class="destructive" :disabled="busy || disabled" @click="remove">确认解除关联</button></div>
        </div>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.associations { display: grid; gap: 14px; min-width: 0; }
.links { display: grid; margin: 0; padding: 0; list-style: none; }
.links li { display: grid; gap: 10px; padding: 14px 0; border-top: 1px solid var(--rule); }
.pair { display: grid; gap: 10px; }
.side { display: flex; gap: 10px; align-items: center; min-width: 0; }
.side > div { display: grid; min-width: 0; }
.side strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.side small { color: var(--ink-2); font-size: 12px; }
.link-foot { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; justify-content: space-between; }
</style>
