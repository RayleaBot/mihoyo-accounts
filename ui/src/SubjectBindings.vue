<script setup lang="ts">
import { ref, watch } from 'vue'
import { errorText, gameNames, type Account } from './model'
interface Binding { uids: string[]; current?: string }
interface Selection { account_ref: string; role_ref: string }
interface Data { items: Account[]; default_roles: Record<string, Selection>; uid_bindings: Record<string, Binding> }
const props = defineProps<{ invoke: <T>(action: string, payload?: Record<string, unknown>) => Promise<T>; disabled: boolean }>()
const actor = ref(''), game = ref('genshin'), uid = ref(''), busy = ref(false), error = ref(''), data = ref<Data | null>(null)
watch(actor, () => { data.value = null })
async function run(action = 'subjects.get', extra: Record<string, unknown> = {}) {
  if (busy.value || props.disabled) return
  busy.value = true; error.value = ''
  const actorID = actor.value.trim()
  try {
    if (action !== 'subjects.get') await props.invoke(action, { actor_id: actorID, ...extra })
    const result = await props.invoke<Data>('subjects.get', { actor_id: actorID })
    if (actor.value.trim() === actorID) data.value = result
  } catch (cause) { error.value = errorText(cause) } finally { busy.value = false }
}
</script>
<template>
  <section>
    <h2>默认角色与 UID 绑定</h2>
    <form class="login-form" @submit.prevent="run()">
      <label>聊天用户 ID<input v-model="actor" :disabled="busy || disabled" required maxlength="256"></label>
      <button :disabled="busy || disabled || !actor.trim()">读取绑定</button>
    </form>
    <p v-if="error" role="alert" class="danger-text">{{ error }}</p>
    <div v-if="data" class="account-detail">
      <div v-for="(name, key) in gameNames" :key="key">
        <h3>{{ name }} · 当前 UID：{{ data.uid_bindings[key]?.current || '未选择' }}</h3>
        <div v-for="bound in data.uid_bindings[key]?.uids ?? []" :key="bound" class="actions">
          <span class="mono">{{ bound }}</span>
          <button :disabled="busy || disabled" @click="run('subjects.uids.use', { game: key, uid: bound })">使用</button>
          <button :disabled="busy || disabled" @click="run('subjects.uids.remove', { game: key, uid: bound })">删除绑定</button>
        </div>
        <template v-for="account in data.items" :key="account.ref">
          <div v-for="role in account.roles.filter(r => r.game === key)" :key="role.ref" class="actions">
            <span>{{ role.nickname }} · {{ role.uid }}</span>
            <span v-if="data.default_roles[key]?.account_ref === account.ref && data.default_roles[key]?.role_ref === role.ref" class="hint">默认角色</span>
            <button v-else :disabled="busy || disabled" @click="run('subjects.select', { account_ref: account.ref, role_ref: role.ref })">设为默认</button>
          </div>
        </template>
      </div>
      <form class="login-form" @submit.prevent="run('subjects.uids.bind', { game, uid })">
        <label>游戏<select v-model="game"><option v-for="(name, key) in gameNames" :key="key" :value="key">{{ name }}</option></select></label>
        <label>UID<input v-model="uid" required inputmode="numeric" maxlength="10"></label>
        <button :disabled="busy || disabled">绑定并使用</button>
      </form>
    </div>
  </section>
</template>
