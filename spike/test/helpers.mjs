import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

export const root = join(dirname(fileURLToPath(import.meta.url)), '..')
export const buildDir = join(root, 'build')
export const addonPath = (name) => join(buildDir, `${name}.node`)
const require = createRequire(import.meta.url)
export const load = (name) => require(addonPath(name))
export const isWin = process.platform === 'win32'
