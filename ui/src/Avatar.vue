<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { Person } from './identity'

const props = defineProps<{ person: Person; size?: number }>()
const failed = ref(false)
watch(() => props.person.avatar, () => { failed.value = false })
const tone = computed(() => [...props.person.seed].reduce((sum, char) => sum + char.charCodeAt(0), 0) % 6)
</script>

<template>
  <span class="avatar" :class="`tone-${tone}`" :style="{ '--avatar-size': `${size ?? 48}px` }" aria-hidden="true">
    <span class="avatar-initial">{{ person.initial }}</span>
    <img v-if="person.avatar && !failed" :src="person.avatar" alt="" loading="lazy" decoding="async" referrerpolicy="no-referrer" @error="failed = true">
  </span>
</template>

<style scoped>
.avatar { position: relative; display: inline-grid; place-items: center; flex: 0 0 auto; width: var(--avatar-size); height: var(--avatar-size); overflow: hidden; border-radius: 50%; background: var(--tone-bg); color: var(--tone-ink); box-shadow: inset 0 0 0 1px rgb(0 0 0 / 6%); font-size: calc(var(--avatar-size) * .38); font-weight: 650; font-variant-numeric: tabular-nums; }
.avatar img { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; }
.tone-0 { --tone-bg: light-dark(#dbe9f8, #1f3550); --tone-ink: light-dark(#1d4f86, #b7d5f5); }
.tone-1 { --tone-bg: light-dark(#e6e1f6, #2e2a4d); --tone-ink: light-dark(#4b3b8f, #cfc6f2); }
.tone-2 { --tone-bg: light-dark(#dcefe6, #1c3a2e); --tone-ink: light-dark(#1f6247, #b4e0cb); }
.tone-3 { --tone-bg: light-dark(#d5edf0, #1d3d40); --tone-ink: light-dark(#1c5d63, #b3e0e3); }
.tone-4 { --tone-bg: light-dark(#ece6dc, #3a352d); --tone-ink: light-dark(#5e5240, #ddd3c3); }
.tone-5 { --tone-bg: light-dark(#e2e6ec, #2a3039); --tone-ink: light-dark(#3d4757, #c9d0db); }
@media (forced-colors: active) { .avatar { border: 1px solid CanvasText; } }
</style>
