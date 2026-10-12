<script setup lang="ts">
import Icon from './Icon.vue'
defineProps<{ error: string; notice: string; busy: boolean }>()
const emit = defineEmits<{ retry: [] }>()
</script>

<template>
  <div v-if="error" class="feedback danger" role="alert">
    <Icon name="alert" />
    <span>{{ error }}</span>
    <button type="button" class="quiet" :disabled="busy" @click="emit('retry')">重试</button>
  </div>
  <p v-if="notice" class="feedback" role="status"><Icon name="check" />{{ notice }}</p>
</template>
