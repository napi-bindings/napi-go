// Costruisce ed esegue i test della suite di Node.js (js-native-api, node-api) portati su sys.
//
//   node tests/nodesuite/run.mjs [opzioni] [filtro...]
//
// Opzioni:
//   --no-build   esegue senza ricompilare gli addon
//   --build-only compila senza eseguire
//   --jobs=N     build in parallelo (default: metà delle CPU)
//
// Un filtro seleziona i test il cui id ("suite/cartella/file") contiene la stringa indicata.
//
// Per ogni cartella di test:
//   - binding.gyp (copiato da Node.js) elenca i target; NAPI_VERSION=N e NAPI_EXPERIMENTAL
//     diventano i build tag napi_go_vN / napi_go_experimental;
//   - il sorgente Go di un target sta in <cartella>/<target>/ oppure, se la cartella ha un solo
//     target, direttamente in <cartella>/;
//   - l'addon va in <cartella>/build/Release/<target>.node, dove lo cercano gli script JS;
//   - ogni file .js è un test, come in test/testpy di Node.js, con le righe "// Flags:" e
//     "// Env:" rispettate;
//   - napi-go.json (facoltativo) aggiunge defines a un target quando il test C li imposta nel
//     sorgente invece che nel binding.gyp: {"targets": {"<target>": {"defines": [...], "reason": "..."}}}.
//
// status.json elenca i test saltati con il motivo, sempre ("skip"), sotto una versione di
// Node.js ("minNodeMajor", per i test di nodejs/node main che usano funzionalità recenti) o su
// alcune piattaforme ("skipPlatforms", per i limiti di piattaforma di napi-go).
// Variabili d'ambiente rispettate: CC,
// GOEXPERIMENT (es. cgocheck2). UV_THREADPOOL_SIZE e NODE_OPTIONS non arrivano agli script.
import { spawn } from 'node:child_process'
import { existsSync, readdirSync, readFileSync, rmSync, mkdirSync, statSync } from 'node:fs'
import { availableParallelism } from 'node:os'
import { dirname, join, relative, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const root = join(here, '..', '..')
const SUITES = ['js-native-api', 'node-api']
const BUILD_SUBDIR = join('build', 'Release')
const TEST_TIMEOUT_MS = 120_000
const EXIT_FAILURE = 1
const MIN_JOBS = 1
const SKIP_MARKER = '1..0 # Skipped'
const FLAGS_PATTERN = /\/\/\s+Flags:(.*)/
const ENV_PATTERN = /\/\/\s+Env:(.*)/
// NAPI_VERSION di default degli header (node-gyp non la definisce) e di sys senza build tag.
const DEFAULT_NAPI_VERSION = 8
const VERSION_TAGS = new Map([[8, null], [9, 'napi_go_v9'], [10, 'napi_go_v10']])
const EXPERIMENTAL_TAG = 'napi_go_experimental'
const NODE_MAJOR = Number(process.versions.node.split('.')[0])
// Variabili d'ambiente tolte ai processi di test, come nella CI di Node.js: cambiano il
// comportamento di libuv o di node e alcuni script ne verificano il valore di default (es.
// node-api/test_uv_threadpool_size). Una riga "// Env:" dello script le può reimpostare.
const STRIPPED_ENV = ['UV_THREADPOOL_SIZE', 'NODE_OPTIONS']
// Piattaforma nel formato di status.skipPlatforms, es. "darwin-x64".
const PLATFORM = `${process.platform}-${process.arch}`

class UsageError extends Error {}

function parseArgs (argv) {
  const opts = { build: true, run: true, jobs: Math.max(MIN_JOBS, Math.floor(availableParallelism() / 2)), filters: [] }
  for (const a of argv) {
    if (a === '--no-build') opts.build = false
    else if (a === '--build-only') opts.run = false
    else if (a.startsWith('--jobs=')) {
      const n = Number(a.slice('--jobs='.length))
      if (!Number.isInteger(n) || n < MIN_JOBS) throw new UsageError(`invalid --jobs value: ${a}`)
      opts.jobs = n
    } else if (a.startsWith('--')) throw new UsageError(`unknown option: ${a}`)
    else opts.filters.push(a)
  }
  return opts
}

// readJSON legge un file JSON; restituisce fallback se il file non esiste.
function readJSON (file, fallback) {
  if (!existsSync(file)) return fallback
  return parseJSON(readText(file), file)
}

// readText legge un file di testo indicando il file nell'errore.
function readText (file) {
  try {
    return readFileSync(file, 'utf8')
  } catch (err) {
    throw new Error(`cannot read ${relative(root, file)}: ${err.message}`)
  }
}

function parseJSON (text, file) {
  try {
    return JSON.parse(text)
  } catch (err) {
    throw new Error(`cannot parse ${relative(root, file)}: ${err.message}`)
  }
}

// parseGyp legge un binding.gyp: è JSON con apici singoli, commenti # e virgole finali.
function parseGyp (file) {
  const text = readText(file)
    .split('\n').map((l) => l.replace(/^\s*#.*$/, '')).join('\n')
    .replace(/'/g, '"')
    .replace(/,(\s*[\]}])/g, '$1')
  return parseJSON(text, file)
}

// targetTags ricava i build tag di sys dai defines del target.
function targetTags (target, gypFile) {
  const defines = target.defines ?? []
  if (defines.includes('NAPI_EXPERIMENTAL')) return [EXPERIMENTAL_TAG]
  const versionDefine = defines.find((d) => d.startsWith('NAPI_VERSION='))
  const version = versionDefine ? Number(versionDefine.split('=')[1]) : DEFAULT_NAPI_VERSION
  if (!VERSION_TAGS.has(version)) {
    throw new Error(`${relative(root, gypFile)}: target ${target.target_name}: NAPI_VERSION=${version} has no napi-go build tag`)
  }
  const tag = VERSION_TAGS.get(version)
  return tag ? [tag] : []
}

function hasGoFiles (dir) {
  return existsSync(dir) && readdirSync(dir).some((f) => f.endsWith('.go'))
}

// discover restituisce le cartelle di test con i loro target e script.
function discover () {
  const dirs = []
  for (const suite of SUITES) {
    const suiteDir = join(here, suite)
    if (!existsSync(suiteDir)) continue
    for (const name of readdirSync(suiteDir).sort()) {
      const dir = join(suiteDir, name)
      if (!statSync(dir).isDirectory()) continue
      const gypFile = join(dir, 'binding.gyp')
      if (!existsSync(gypFile)) continue
      const gyp = parseGyp(gypFile)
      const overrides = readJSON(join(dir, 'napi-go.json'), { targets: {} }).targets ?? {}
      const targets = (gyp.targets ?? []).map((gypTarget) => {
        const extra = overrides[gypTarget.target_name]?.defines ?? []
        const t = { ...gypTarget, defines: [...extra, ...(gypTarget.defines ?? [])] }
        const own = join(dir, t.target_name)
        const pkgDir = hasGoFiles(own) ? own : dir
        if (!hasGoFiles(pkgDir)) {
          throw new Error(`${relative(root, dir)}: no Go sources for target ${t.target_name}`)
        }
        if (pkgDir === dir && gyp.targets.length > 1) {
          throw new Error(`${relative(root, dir)}: ${gyp.targets.length} targets need one subdirectory each`)
        }
        return { name: t.target_name, pkg: './' + relative(root, pkgDir).split(sep).join('/'), tags: targetTags(t, gypFile), out: join(dir, BUILD_SUBDIR, `${t.target_name}.node`) }
      })
      const scripts = readdirSync(dir).filter((f) => f.endsWith('.js')).sort()
      dirs.push({ id: `${suite}/${name}`, dir, targets, scripts })
    }
  }
  return dirs
}

function matches (id, filters) {
  return filters.length === 0 || filters.some((f) => id.includes(f))
}

// run esegue un comando e ne raccoglie l'esito senza lanciare eccezioni.
function run (cmd, args, options) {
  return new Promise((resolve) => {
    const child = spawn(cmd, args, { ...options, stdio: ['ignore', 'pipe', 'pipe'] })
    let stdout = ''
    let stderr = ''
    let timedOut = false
    const timer = options.timeout ? setTimeout(() => { timedOut = true; child.kill('SIGKILL') }, options.timeout) : null
    child.stdout.on('data', (d) => { stdout += d })
    child.stderr.on('data', (d) => { stderr += d })
    child.on('error', (err) => { clearTimeout(timer); resolve({ code: null, signal: null, stdout, stderr: stderr + err.message, timedOut }) })
    child.on('close', (code, signal) => { clearTimeout(timer); resolve({ code, signal, stdout, stderr, timedOut }) })
  })
}

async function buildTarget (t) {
  mkdirSync(dirname(t.out), { recursive: true })
  const args = ['build', '-buildmode=c-shared', '-buildvcs=false', '-trimpath', '-ldflags=-s -w']
  if (t.tags.length) args.push(`-tags=${t.tags.join(',')}`)
  args.push('-o', t.out, t.pkg)
  const r = await run('go', args, { cwd: root, env: { ...process.env, CGO_ENABLED: '1' } })
  // c-shared produce anche l'header C delle export: non serve.
  const header = t.out.replace(/\.node$/, '.h')
  if (existsSync(header)) rmSync(header)
  return r
}

// pool esegue fn su items con al massimo jobs esecuzioni contemporanee.
async function pool (items, jobs, fn) {
  const results = new Array(items.length)
  let next = 0
  const worker = async () => {
    while (next < items.length) {
      const i = next++
      results[i] = await fn(items[i])
    }
  }
  await Promise.all(Array.from({ length: Math.min(jobs, items.length) }, worker))
  return results
}

function scriptConfig (file) {
  const source = readText(file)
  const flags = source.match(FLAGS_PATTERN)?.[1].trim().split(/\s+/).filter(Boolean) ?? []
  const env = {}
  for (const pair of source.match(ENV_PATTERN)?.[1].trim().split(/\s+/).filter(Boolean) ?? []) {
    const eq = pair.indexOf('=')
    if (eq <= 0) throw new Error(`${relative(root, file)}: malformed Env entry "${pair}"`)
    env[pair.slice(0, eq)] = pair.slice(eq + 1)
  }
  return { flags, env }
}

// testEnv è l'ambiente di un processo di test: quello del runner senza STRIPPED_ENV, più le
// variabili della riga "// Env:".
function testEnv (scriptEnv) {
  const env = { ...process.env }
  for (const name of STRIPPED_ENV) delete env[name]
  return { ...env, ...scriptEnv }
}

function report (label, id, r) {
  process.stdout.write(`${label} ${id}\n`)
  if (!r) return
  const reason = r.timedOut ? `timeout after ${TEST_TIMEOUT_MS} ms` : `exit code=${r.code} signal=${r.signal}`
  process.stdout.write(`  ${reason}\n`)
  if (r.stdout.trim()) process.stdout.write(`  --- stdout\n${r.stdout.trimEnd()}\n`)
  if (r.stderr.trim()) process.stdout.write(`  --- stderr\n${r.stderr.trimEnd()}\n`)
}

// skipReason restituisce il motivo per cui il test id va saltato, oppure null:
//   status.skip[id]          = "motivo"                          sempre
//   status.minNodeMajor[id]  = {"major": N, "reason": "motivo"}  con Node < N
//   status.skipPlatforms[id] = {"platforms": ["darwin-x64"], "reason": "motivo"}  su quelle piattaforme
function skipReason (status, id) {
  if (status.skip?.[id]) return status.skip[id]
  const min = status.minNodeMajor?.[id]
  if (min && NODE_MAJOR < min.major) return `requires Node.js >= ${min.major}: ${min.reason}`
  const platforms = status.skipPlatforms?.[id]
  if (platforms?.platforms?.includes(PLATFORM)) return `not supported on ${PLATFORM}: ${platforms.reason}`
  return null
}

async function main () {
  const opts = parseArgs(process.argv.slice(2))
  const status = readJSON(join(here, 'status.json'), {})
  const dirs = discover().filter((d) => matches(d.id, opts.filters) || d.scripts.some((s) => matches(`${d.id}/${s.replace(/\.js$/, '')}`, opts.filters)))
  if (dirs.length === 0) throw new UsageError('no test matches the given filters')

  let failures = 0
  if (opts.build) {
    const targets = dirs.flatMap((d) => d.targets.map((t) => ({ ...t, id: `${d.id}:${t.name}` })))
    const t0 = Date.now()
    const results = await pool(targets, opts.jobs, buildTarget)
    results.forEach((r, i) => {
      if (r.code !== 0) {
        failures++
        report('BUILD FAIL', targets[i].id, r)
      }
    })
    process.stdout.write(`built ${targets.length - failures}/${targets.length} addons in ${Date.now() - t0} ms\n`)
    if (failures) process.exit(EXIT_FAILURE)
  }
  if (!opts.run) return

  const counts = { pass: 0, skip: 0, fail: 0 }
  for (const d of dirs) {
    for (const script of d.scripts) {
      const id = `${d.id}/${script.replace(/\.js$/, '')}`
      if (!matches(id, opts.filters) && !matches(d.id, opts.filters)) continue
      const reason = skipReason(status, id)
      if (reason) {
        counts.skip++
        process.stdout.write(`skip ${id} (${reason})\n`)
        continue
      }
      const file = join(d.dir, script)
      const { flags, env } = scriptConfig(file)
      const r = await run(process.execPath, [...flags, file], { cwd: here, env: testEnv(env), timeout: TEST_TIMEOUT_MS })
      if (r.code === 0 && !r.timedOut) {
        const skipped = r.stdout.startsWith(SKIP_MARKER)
        counts[skipped ? 'skip' : 'pass']++
        process.stdout.write(`${skipped ? 'skip' : 'ok  '} ${id}${skipped ? ` (${r.stdout.slice(SKIP_MARKER.length).replace(/^:\s*/, '').trim()})` : ''}\n`)
      } else {
        counts.fail++
        report('FAIL', id, r)
      }
    }
  }
  process.stdout.write(`\n${counts.pass} passed, ${counts.skip} skipped, ${counts.fail} failed\n`)
  if (counts.fail) process.exit(EXIT_FAILURE)
}

main().catch((err) => {
  process.stderr.write(`${err instanceof UsageError ? 'usage error' : 'error'}: ${err.message}\n`)
  process.exit(EXIT_FAILURE)
})
