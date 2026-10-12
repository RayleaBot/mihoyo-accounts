<script setup lang="ts">
// One stroke family for every glyph on the page: 20px grid, 1.6px round strokes.
const paths = {
  qr: 'M3.5 3.5h4.5v4.5H3.5z M12 3.5h4.5v4.5H12z M3.5 12h4.5v4.5H3.5z M12 12h1.8v1.8H12z M14.7 14.7h1.8v1.8h-1.8z M12 16.5v0 M16.5 12v0',
  import: 'M10 3v8.5 M6.6 8.4 10 11.8l3.4-3.4 M3.8 12.5v2.7a1.3 1.3 0 0 0 1.3 1.3h9.8a1.3 1.3 0 0 0 1.3-1.3v-2.7',
  search: 'M14.2 8.8a5.4 5.4 0 1 1-10.8 0 5.4 5.4 0 0 1 10.8 0z M12.7 12.7 16.6 16.6',
  refresh: 'M15.8 10a5.8 5.8 0 0 1-10 4 M4.2 10a5.8 5.8 0 0 1 10-4 M14.6 3.2V6.2h-3 M5.4 16.8v-3h3',
  shield: 'M10 2.8 4.3 5v4.4c0 3.6 2.4 6.2 5.7 7.6 3.3-1.4 5.7-4 5.7-7.6V5z M7.4 10l1.9 1.9 3.4-3.6',
  chevron: 'M8 5l5 5-5 5',
  back: 'M16 10H4.5 M9 5.5 4.5 10 9 14.5',
  close: 'M5.5 5.5l9 9 M14.5 5.5l-9 9',
  lock: 'M5.6 9h8.8a1.1 1.1 0 0 1 1.1 1.1v5.8a1.1 1.1 0 0 1-1.1 1.1H5.6a1.1 1.1 0 0 1-1.1-1.1v-5.8A1.1 1.1 0 0 1 5.6 9z M7 9V6.6a3 3 0 0 1 6 0V9',
  key: 'M13 3.6a3.4 3.4 0 1 1 0 6.8 3.4 3.4 0 0 1 0-6.8z M10.6 9.4 3.6 16.4 M5.6 14.4l1.9 1.9 M7.6 12.4l1.5 1.5',
  link: 'M8.6 11.4a3 3 0 0 0 4.2 0l2.5-2.5a3 3 0 0 0-4.2-4.2L10 5.8 M11.4 8.6a3 3 0 0 0-4.2 0l-2.5 2.5a3 3 0 0 0 4.2 4.2l1.1-1.1',
  trash: 'M4 6h12 M8 6V4.4h4V6 M5.6 6l.7 9.7a1.1 1.1 0 0 0 1.1 1h5.2a1.1 1.1 0 0 0 1.1-1L14.4 6',
  plus: 'M10 4.5v11 M4.5 10h11',
  chart: 'M3.5 16.5h13 M6 13.5V9.5 M10 13.5V5.5 M14 13.5v-2.5',
  check: 'M4.6 10.4 8.1 13.9 15.4 6.1',
  alert: 'M10 3.6 17 16H3z M10 8.6v3.3 M10 14.1v.2',
  device: 'M7 2.8h6a1.1 1.1 0 0 1 1.1 1.1v12.2a1.1 1.1 0 0 1-1.1 1.1H7a1.1 1.1 0 0 1-1.1-1.1V3.9A1.1 1.1 0 0 1 7 2.8z M9 14.6h2',
  cloud: 'M6.2 15.4h8.2a3 3 0 0 0 .3-6 4.4 4.4 0 0 0-8.5-1 3.5 3.5 0 0 0 0 7z',
  grid: 'M4 4h4.8v4.8H4z M11.2 4H16v4.8h-4.8z M4 11.2h4.8V16H4z M11.2 11.2H16V16h-4.8z',
} as const

defineProps<{ name: keyof typeof paths; size?: number }>()
</script>

<template>
  <svg class="icon" viewBox="0 0 20 20" :width="size ?? 18" :height="size ?? 18" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false"><path :d="paths[name]" /></svg>
</template>
