<script setup lang="ts">
import { onMounted, ref } from 'vue'
import type { PluginUIClient } from '@rayleabot/plugin-ui'
import { errorText, gameNames } from './model'
const props = defineProps<{ client: PluginUIClient; disabled: boolean }>()
const entries = ref<{ plugin_id: string; games: string[] }[]>([]), busy = ref(false), error = ref(''), notice = ref(''), loaded = ref(false)
async function load() {
  busy.value = true; error.value = ''
  try { entries.value = (await props.client.reloadSettings()).config.allowed_plugins as typeof entries.value; loaded.value = true }
  catch (cause) { error.value = errorText(cause) } finally { busy.value = false }
}
async function save() {
  busy.value = true; error.value = ''; notice.value = ''
  try { await props.client.saveSettings({ allowed_plugins: entries.value }); notice.value = '全局授权已保存。' }
  catch (cause) { error.value = errorText(cause) } finally { busy.value = false }
}
onMounted(load)
</script>
<template>
  <section>
    <div class="section-heading"><h2>允许调用的插件</h2><button :disabled="busy || disabled" @click="load">重新读取</button></div>
    <p class="hint">适用于全部机器人；每个米游社账号仍需单独授权。</p>
    <form v-if="loaded" @submit.prevent="save">
      <fieldset :disabled="busy || disabled">
        <div v-for="(entry, index) in entries" :key="index" class="account-detail">
          <label>插件 ID<input v-model="entry.plugin_id" required pattern="[a-zA-Z0-9_.-]+"></label>
          <div class="checks"><label v-for="(name, game) in gameNames" :key="game"><input v-model="entry.games" type="checkbox" :value="game">{{ name }}</label></div>
          <button type="button" @click="entries.splice(index, 1)">移除此插件</button>
        </div>
        <div class="actions"><button type="button" @click="entries.push({ plugin_id: '', games: [] })">添加插件</button><button type="submit" class="primary">保存全局授权</button></div>
      </fieldset>
    </form>
    <p v-if="error" role="alert" class="danger-text">{{ error }}</p><p v-if="notice" role="status">{{ notice }}</p>
  </section>
</template>
