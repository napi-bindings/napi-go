// Build degli addon dello spike (prototipo del futuro `napi-go build`).
//   node scripts/build.mjs            → build/basic.node, build/second.node, build/cref.node
// Variabili: CC (compilatore C per cgo, es. "zig cc"), GO (default "go").
import { spawnSync } from 'node:child_process'
import { mkdirSync, rmSync, existsSync, statSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const out = join(root, 'build')
mkdirSync(out, { recursive: true })

function run(cmd, args, env = {}) {
  const t0 = Date.now()
  const r = spawnSync(cmd, args, { cwd: root, stdio: 'inherit', env: { ...process.env, ...env } })
  if (r.error) { // eseguibile non trovato o spawn fallito
    console.error(`✗ impossibile eseguire ${cmd}: ${r.error.message}`)
    process.exit(1)
  }
  if (r.status !== 0) {
    console.error(`✗ ${cmd} ${args.join(' ')} (exit ${r.status})`)
    process.exit(r.status ?? 1)
  }
  return Date.now() - t0
}

const go = process.env.GO || 'go'
for (const name of ['basic', 'second']) {
  const target = join(out, `${name}.node`)
  // -buildvcs=false: le info VCS non servono in un .node e la build non deve dipendere da git
  // (nei container CI il repository ha un altro proprietario → "dubious ownership", exit 128).
  const ms = run(go, ['build', '-buildmode=c-shared', '-buildvcs=false', '-trimpath', '-ldflags=-s -w', '-o', target, `./addons/${name}`], { CGO_ENABLED: '1' })
  rmSync(join(out, `${name}.h`), { force: true }) // header generato da c-shared: inutile per un .node
  console.log(`✓ ${name}.node  ${(statSync(target).size / 1048576).toFixed(2)} MB  (${ms} ms)`)
}

// Baseline in C puro (solo Unix: su Windows servirebbe node.lib).
if (process.platform !== 'win32') {
  const cc = process.env.CREF_CC || 'cc'
  const extra = process.platform === 'darwin' ? ['-undefined', 'dynamic_lookup'] : []
  run(cc, ['-O2', '-shared', '-fPIC', '-Inapi/include', 'addons/cref/cref.c', '-o', join(out, 'cref.node'), ...extra])
  console.log('✓ cref.node (baseline C)')
} else if (existsSync(join(out, 'cref.node'))) {
  rmSync(join(out, 'cref.node'))
}
