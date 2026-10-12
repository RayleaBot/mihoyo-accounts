<script setup lang="ts">
import { computed, useId } from 'vue'
import Avatar from './Avatar.vue'
import Icon from './Icon.vue'
import type { Person } from './identity'
import { flagOf, gameShortNames, proofOf, realmName, statusOf, type Account } from './model'

const props = defineProps<{ account: Account; person: Person; busy: boolean; marked?: boolean; large?: boolean; transition?: boolean; owned?: number }>()
const emit = defineEmits<{ mark: [value: boolean]; check: []; refresh: []; pull: [] }>()
const nameID = useId()
const flag = computed(() => flagOf(props.account))
const limit = computed(() => props.large ? Infinity : 4)
const roles = computed(() => props.account.roles.slice(0, limit.value))
const hidden = computed(() => props.account.roles.length - roles.value.length)
const showID = computed(() => !props.person.name.startsWith(`${props.person.idLabel} `))
const contact = computed(() => showID.value || (props.owned ?? 0) > 1)
</script>

<template>
  <article class="card" :class="{ marked, large, flagged: flag }" :style="transition ? { viewTransitionName: 'account-card' } : undefined" :aria-labelledby="nameID">
    <span v-if="flag" class="flag" :class="flag.tone">{{ flag.text }}</span>
    <header class="person">
      <Avatar :person="person" :size="large ? 60 : 48" />
      <div class="person-text">
        <h3 :id="nameID" class="name" :title="person.name">{{ person.name }}</h3>
        <p v-if="contact" class="contact" :title="`${person.idLabel} ${person.id}`"><template v-if="showID">{{ person.idLabel }} <span class="num">{{ person.id }}</span></template><template v-if="(owned ?? 0) > 1"><template v-if="showID"> · </template>共 <span class="num">{{ owned }}</span> 个通行证</template></p>
      </div>
      <label v-if="!large" class="mark">
        <input type="checkbox" :checked="marked" :disabled="busy" :aria-label="`选择 ${person.name} 的${realmName(account)}账号 ${account.account_label}`" @change="emit('mark', ($event.target as HTMLInputElement).checked)">
      </label>
    </header>
    <dl class="ledger">
      <div class="row pass"><dt>{{ realmName(account) }}通行证</dt><dd class="num">{{ account.account_label }}</dd></div>
      <div v-for="role in roles" :key="role.ref" class="row">
        <dt>{{ gameShortNames[role.game] || role.game }}</dt>
        <dd class="role-name" :title="role.nickname">{{ role.nickname }}</dd>
        <dd class="level"><span class="sr-only">等级 </span>Lv.{{ role.level }}</dd>
        <dd class="num"><span class="sr-only">UID </span>{{ role.uid }}</dd>
      </div>
      <div v-if="!account.roles.length" class="row note"><dt>角色</dt><dd>尚未发现游戏角色</dd></div>
      <div v-else-if="hidden > 0" class="row note"><dt></dt><dd>另有 {{ hidden }} 个角色</dd></div>
    </dl>
    <footer class="foot">
      <p class="state">
        <Icon v-if="!flag" name="check" :size="16" />
        <strong>{{ statusOf(account) }}</strong>
        <span class="proof">{{ proofOf(account) }}</span>
      </p>
      <div class="acts">
        <button type="button" class="quiet" :disabled="busy" :aria-label="`检查 ${person.name} 的登录状态`" @click="emit('check')"><Icon name="shield" :size="16" />检查状态</button>
        <button type="button" class="quiet" :disabled="busy || !account.can_refresh" :aria-label="`刷新 ${person.name} 的凭据`" :title="account.can_refresh ? '重新换取 LToken 与 Cookie Token' : '缺少 SToken，无法刷新'" @click="emit('refresh')"><Icon name="refresh" :size="16" />刷新凭据</button>
        <button v-if="!large" type="button" class="quiet pull" :disabled="busy" :aria-label="`管理 ${person.name} 的账号`" @click="emit('pull')">管理<Icon name="chevron" :size="16" /></button>
      </div>
    </footer>
  </article>
</template>

<style scoped>
.card { position: relative; display: grid; grid-template-rows: auto 1fr auto; gap: 14px; min-width: 0; padding: 18px 18px 12px; border-radius: 14px; background: var(--card); box-shadow: var(--card-shadow); }
.card.marked { box-shadow: 0 0 0 2px var(--ink), var(--card-shadow); }
.card.large { box-shadow: 0 0 0 2px var(--accent), var(--card-shadow); }
.flag { position: absolute; z-index: -1; top: -25px; left: 22px; padding: 3px 10px 16px; border-radius: 7px 7px 0 0; font-size: 12px; font-weight: 650; line-height: 18px; letter-spacing: .02em; }
.flag.red { background: var(--flag-red); color: var(--on-flag-red); }
.flag.amber { background: var(--flag-amber); color: var(--on-flag-amber); }
.person { display: flex; gap: 14px; align-items: center; min-width: 0; }
.person-text { flex: 1; min-width: 0; }
.name { overflow: hidden; margin: 0; font-size: 18px; font-weight: 650; line-height: 1.35; text-overflow: ellipsis; white-space: nowrap; }
.large .name { font-size: 22px; }
.contact { overflow: hidden; margin: 2px 0 0; color: var(--ink-2); font-size: 13px; text-overflow: ellipsis; white-space: nowrap; }
.mark { display: grid; place-items: center; align-self: flex-start; width: 36px; height: 36px; margin: -8px -8px 0 0; border-radius: 8px; cursor: pointer; }
.mark:hover { background: var(--hover); }
.ledger { display: grid; align-content: start; margin: 0; font-size: 13px; }
.row { display: grid; grid-template-columns: 3.6em minmax(0, 1fr) 4.4em 7.2em; gap: 8px; align-items: baseline; padding: 6px 0; border-top: 1px solid var(--rule); }
.row dt { color: var(--ink-2); }
.row dd { overflow: hidden; margin: 0; text-overflow: ellipsis; white-space: nowrap; }
.row.pass dt { grid-column: 1 / 4; }
.row.note dd { grid-column: 2 / -1; color: var(--ink-2); }
.level { color: var(--ink-2); text-align: right; }
.num { font-variant-numeric: tabular-nums; text-align: right; letter-spacing: .01em; }
.contact .num { text-align: left; }
.foot { display: grid; gap: 4px; padding-top: 10px; border-top: 1px solid var(--rule); }
.state { display: flex; flex-wrap: wrap; gap: 2px 8px; align-items: center; min-width: 0; margin: 0; color: var(--ink-2); font-size: 13px; }
.state strong { color: var(--ink); font-weight: 650; }
.state .icon { margin-right: -4px; color: var(--ink-2); }
.acts { display: flex; gap: 2px; margin: 0 -10px; }
.acts button { font-weight: 500; }
.acts .icon { color: var(--ink-2); }
.acts button:hover:not(:disabled) .icon { color: var(--ink); }
.pull { margin-left: auto; }
@media (forced-colors: active) { .card { border: 1px solid CanvasText; } .card.marked { outline: 3px solid Highlight; } .flag { border: 1px solid CanvasText; } }
</style>
