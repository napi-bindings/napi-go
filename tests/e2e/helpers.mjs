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
// Versione Node-API supportata dal Node che esegue i test.
export const hostNapiVersion = Number(process.versions.napi)

const CHILD_TIMEOUT_MS = 60_000

// nodeEval esegue codice in un processo Node separato e ne restituisce l'esito. Allo scadere di
// timeout il figlio riceve SIGKILL: un processo bloccato può non reagire a SIGTERM.
export function nodeEval (code, { nodeArgs = [], timeout = CHILD_TIMEOUT_MS } = {}) {
  const r = spawnSync(process.execPath, [...nodeArgs, '-e', code], { encoding: 'utf8', timeout, killSignal: 'SIGKILL' })
  if (r.error) throw r.error
  return r
}

// lastJSONLine estrae l'ultima riga JSON dello stdout di un figlio.
export function lastJSONLine (r) {
  const line = (r.stdout || '').trim().split('\n').filter(Boolean).pop()
  if (!line) {
    throw new Error(`il figlio non ha prodotto output: code=${r.status} sig=${r.signal} stderr=${r.stderr}`)
  }
  return JSON.parse(line)
}

const READY = 'ready'
// Codice di uscita del figlio se il segnale non lo termina prima della fine della sua vita.
const LOST_SIGNAL_EXIT = 99

// signalChild avvia un figlio che carica l'addon, aspetta che sia pronto, gli invia signal e
// restituisce come è terminato. Se non termina entro hangMs viene ucciso con SIGKILL.
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
