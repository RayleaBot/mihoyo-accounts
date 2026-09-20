import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vitest/config'

export default defineConfig({ base: './', plugins: [vue()], resolve: { dedupe: ['vue'] }, test: { environment: 'jsdom', include: ['tests/**/*.test.ts'] } })
