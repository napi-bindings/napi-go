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
// Node.js ("minNodeMajor" o, quando la funzionalità arriva con una minor diversa per ogni linea di
// release, "minNodeVersion"; servono per i test di nodejs/node main che usano funzionalità
// recenti) o su alcune piattaforme ("skipPlatforms", per i limiti di piattaforma di napi-go).
// Variabili d'ambiente rispettate: CC,
// GOEXPERIMENT (es. cgocheck2). UV_THREADPOOL_SIZE e NODE_OPTIONS non arrivano agli script.
import { spawn, spawnSync } from 'node:child_process'
import { existsSync, readdirSync, readFileSync, rmSync, mkdirSync, statSync } from 'node:fs'
import { availableParallelism } from 'node:os'
import { dirname, join, relative, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const root = join(here, '..', '..')
const SUITES = ['js-native-api', 'node-api']
const BUILD_SUBDIR = join('build', 'Release')
const TEST_TIMEOUT_MS = 120_000
// Durata di sample(1) per processo e suo timeout, nella diagnosi di un test bloccato (macOS).
const SAMPLE_SECONDS = 1
const SAMPLE_TIMEOUT_MS = 30_000
const isWindows = process.platform === 'win32'
// Attesa massima di 'close' dopo aver terminato un test scaduto.
const KILL_GRACE_MS = 5_000
const EXIT_FAILURE = 1
const MIN_JOBS = 1
const SKIP_MARKER = '1..0 # Skipped'
const FLAGS_PATTERN = /\/\/\s+Flags:(.*)/
const ENV_PATTERN = /\/\/\s+Env:(.*)/
// NAPI_VERSION di default degli header (node-gyp non la definisce) e di sys senza build tag.
const DEFAULT_NAPI_VERSION = 8
// 2147483646 (NAPI_VERSION_EXPERIMENTAL - 1) è la versione mai supportata di
// node-api/test_module_version_mismatch: sys la dichiara solo con un build tag riservato ai test.
const VERSION_TAGS = new Map([[8, null], [9, 'napi_go_v9'], [10, 'napi_go_v10'], [2147483646, 'napi_go_test_version_mismatch']])
const EXPERIMENTAL_TAG = 'napi_go_experimental'
// NODE_VERSION è process.versions.node senza il suffisso di prerelease (es. "27.0.0-alpha.0.0.0"
// → "27.0.0"), che compareVersions non saprebbe confrontare.
const NODE_VERSION = process.versions.node.replace(/-.*$/, '')
const NODE_MAJOR = Number(NODE_VERSION.split('.')[0])
// Parti di una versione Node.js confrontate da compareVersions: major, minor, patch.
const VERSION_PARTS = 3
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

// run esegue un comando e ne raccoglie l'esito senza lanciare eccezioni. Su POSIX il processo
// guida un proprio gruppo: allo scadere del timeout si raccolgono informazioni sui processi
// ancora vivi (diagnoseHang) e si termina tutto il gruppo, perché i processi figli lanciati dagli
// script (spawnSync, worker in un figlio) ereditano le pipe e altrimenti 'close' non arriverebbe
// mai. Su Windows si termina l'albero con taskkill.
function run (cmd, args, options) {
  return new Promise((resolve) => {
    const child = spawn(cmd, args, { ...options, stdio: ['ignore', 'pipe', 'pipe'], detached: !isWindows })
    let stdout = ''
    let stderr = ''
    let timedOut = false
    let diagnostics = ''
    let exit = { code: null, signal: null }
    let graceTimer = null
    const finish = (r) => { clearTimeout(timer); clearTimeout(graceTimer); resolve(r) }
    const timer = options.timeout
      ? setTimeout(() => {
        timedOut = true
        diagnostics = diagnoseHang(child.pid)
        killTree(child)
        // Se un discendente fuori dal gruppo tiene aperte le pipe, 'close' non arriva: dopo
        // KILL_GRACE_MS si chiude comunque con l'output raccolto.
        graceTimer = setTimeout(() => finish({ ...exit, stdout, stderr, timedOut, diagnostics }), KILL_GRACE_MS)
      }, options.timeout)
      : null
    child.stdout.on('data', (d) => { stdout += d })
    child.stderr.on('data', (d) => { stderr += d })
    child.on('exit', (code, signal) => { exit = { code, signal } })
    child.on('error', (err) => finish({ code: null, signal: null, stdout, stderr: stderr + err.message, timedOut, diagnostics }))
    child.on('close', (code, signal) => finish({ code, signal, stdout, stderr, timedOut, diagnostics }))
  })
}

// killTree termina child e tutti i suoi discendenti.
function killTree (child) {
  if (isWindows) {
    const r = spawnSync('taskkill', ['/pid', String(child.pid), '/T', '/F'], { encoding: 'utf8' })
    if (r.error || r.status !== 0) child.kill('SIGKILL')
    return
  }
  try {
    process.kill(-child.pid, 'SIGKILL')
  } catch (err) {
    // Il gruppo può essere già vuoto (ESRCH): resta il kill del solo processo.
    if (err.code !== 'ESRCH') process.stderr.write(`cannot kill process group ${child.pid}: ${err.message}\n`)
    child.kill('SIGKILL')
  }
}

// diagnoseHang descrive i processi del gruppo pgid ancora vivi: l'elenco con ps e, su macOS, lo
// stack nativo di ogni thread con sample(1), per capire dove si è fermato un test. Restituisce un
// testo vuoto su Windows o se gli strumenti mancano.
function diagnoseHang (pgid) {
  if (isWindows) return ''
  const ps = spawnSync('ps', ['-ax', '-o', 'pid=,pgid=,ppid=,stat=,command='], { encoding: 'utf8' })
  if (ps.error || ps.status !== 0) return ''
  const procs = ps.stdout.split('\n').map((l) => l.trim().split(/\s+/)).filter((f) => Number(f[1]) === pgid)
  let out = `processes in group ${pgid}:\n` + procs.map((f) => '  ' + f.join(' ')).join('\n') + '\n'
  if (process.platform === 'darwin') {
    for (const f of procs) {
      const sample = spawnSync('sample', [f[0], String(SAMPLE_SECONDS)], { encoding: 'utf8', timeout: SAMPLE_TIMEOUT_MS })
      out += `--- sample ${f[0]}\n` + (sample.stdout || sample.stderr || (sample.error ? sample.error.message : '')) + '\n'
    }
  }
  return out
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
  if (r.diagnostics) process.stdout.write(`  --- diagnostics\n${r.diagnostics.trimEnd()}\n`)
}

// skipReason restituisce il motivo per cui il test id va saltato, oppure null:
//   status.skip[id]          = "motivo"                          sempre
//   status.minNodeMajor[id]  = {"major": N, "reason": "motivo"}  con Node < N
//   status.minNodeVersion[id] = {"versions": ["22.23.3", "24.16.0"], "reason": "motivo"}
//                               (vedi versionSupported)
//   status.skipPlatforms[id] = {"platforms": ["darwin-x64"], "reason": "motivo"}  su quelle piattaforme
function skipReason (status, id) {
  if (status.skip?.[id]) return status.skip[id]
  const min = status.minNodeMajor?.[id]
  if (min && NODE_MAJOR < min.major) return `requires Node.js >= ${min.major}: ${min.reason}`
  const minVersion = status.minNodeVersion?.[id]
  if (minVersion && !versionSupported(NODE_VERSION, minVersion.versions)) {
    return `requires Node.js ${minVersion.versions.map((v) => '>= ' + v).join(' or ')} in its release line: ${minVersion.reason}`
  }
  const platforms = status.skipPlatforms?.[id]
  if (platforms?.platforms?.includes(PLATFORM)) return `not supported on ${PLATFORM}: ${platforms.reason}`
  return null
}

// compareVersions confronta due versioni "major.minor.patch" (senza prefisso v né suffissi).
function compareVersions (a, b) {
  const pa = a.split('.').map(Number)
  const pb = b.split('.').map(Number)
  for (let i = 0; i < VERSION_PARTS; i++) {
    if (pa[i] !== pb[i]) return pa[i] - pb[i]
  }
  return 0
}

// versionSupported dice se version soddisfa un elenco di minimi, uno per linea di release:
// con un minimo per la stessa major serve version >= minimo; una major senza minimo è
// supportata solo se più recente di tutte quelle elencate (le linee intermedie non hanno la
// funzionalità).
function versionSupported (version, minimums) {
  const major = Number(version.split('.')[0])
  const sameLine = minimums.find((m) => Number(m.split('.')[0]) === major)
  if (sameLine) return compareVersions(version, sameLine) >= 0
  return minimums.every((m) => Number(m.split('.')[0]) < major)
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
