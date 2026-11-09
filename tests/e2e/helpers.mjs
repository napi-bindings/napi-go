import { createRequire } from 'node:module'
import { spawn, spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

export const buildDir = join(dirname(fileURLToPath(import.meta.url)), 'build')
export const addonPath = (name) => join(buildDir, `${name}.node`)
const require = createRequire(import.meta.url)
export const load = (name) => require(addonPath(name))

export const isWin = process.platform === 'win32'
export const isDarwinX64 = process.platform === 'darwin' && process.arch === 'x64'
// Node-API version supported by the Node binary running the tests.
export const hostNapiVersion = Number(process.versions.napi)

const CHILD_TIMEOUT_MS = 60_000

// nodeEval runs code in a separate Node process and returns its result. When the timeout expires,
// the child receives SIGKILL because a wedged process may ignore SIGTERM. env adds variables to
// the child's environment.
export function nodeEval (code, { nodeArgs = [], timeout = CHILD_TIMEOUT_MS, env } = {}) {
  const r = spawnSync(process.execPath, [...nodeArgs, '-e', code], {
    encoding: 'utf8', timeout, killSignal: 'SIGKILL', env: env ? { ...process.env, ...env } : undefined,
  })
  if (r.error) throw r.error
  return r
}

// lastJSONLine extracts the last JSON line from a child process's stdout.
export function lastJSONLine (r) {
  const line = (r.stdout || '').trim().split('\n').filter(Boolean).pop()
  if (!line) {
    throw new Error(`child produced no output: code=${r.status} sig=${r.signal} stderr=${r.stderr}`)
  }
  return JSON.parse(line)
}

const READY = 'ready'
// Exit code used by the child if the signal does not terminate it before its lifetime expires.
const LOST_SIGNAL_EXIT = 99

// signalChild spawns a child that loads the addon, waits until it is ready, sends `signal`, and
// returns how it exited. If it does not exit within hangMs, it is killed with SIGKILL.
export function signalChild (setup, signal, { hangMs, lifetimeMs }) {
  return new Promise((resolve, reject) => {
    const script = `${setup}; process.stdout.write('${READY}\\n'); setTimeout(() => process.exit(${LOST_SIGNAL_EXIT}), ${lifetimeMs})`
    const child = spawn(process.execPath, ['-e', script], { stdio: ['ignore', 'pipe', 'pipe'] })
    let stderr = ''
    let sentAt = 0
    let killer = null
    child.stderr.on('data', (d) => { stderr += d })
    child.stdout.on('data', (d) => {
      if (sentAt || !String(d).includes(READY)) return
      sentAt = Date.now()
      child.kill(signal)
      killer = setTimeout(() => child.kill('SIGKILL'), hangMs)
    })
    child.on('error', reject)
    child.on('exit', (code, sig) => {
      clearTimeout(killer)
      resolve({ code, sig, stderr, exitMs: sentAt ? Date.now() - sentAt : -1 })
    })
  })
}
