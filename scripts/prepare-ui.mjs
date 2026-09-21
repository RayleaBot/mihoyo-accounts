import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
await fs.copyFile(path.join(root,'info.json'),path.join(root,'internal/assets/manifest.json'))
const source = path.resolve(process.argv[2] || path.join(root, '../../RayleaBot/sdk/vue'))
const manifest = JSON.parse(await fs.readFile(path.join(source, 'package.json'), 'utf8'))
if (manifest.name !== '@rayleabot/plugin-ui') throw new Error('The source must be the RayleaBot Vue SDK directory.')
const destination = path.join(root, '.rayleabot/sdk/vue')
await fs.mkdir(destination, { recursive: true })
for (const name of ['src', 'package.json', 'tsconfig.json', 'tsconfig.build.json']) {
  await fs.cp(path.join(source, name), path.join(destination, name), { recursive: true })
}
process.stdout.write('Prepared the local Vue SDK for the plugin build.\n')
