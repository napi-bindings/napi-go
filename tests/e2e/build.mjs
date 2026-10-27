// Costruisce gli addon di test end-to-end in tests/e2e/build/.
//
//   node tests/e2e/build.mjs
//
// Variabili d'ambiente rispettate: CC (es. "zig cc" su Windows), GOEXPERIMENT (es. cgocheck2).
import { spawnSync } from 'node:child_process'
import { mkdirSync, rmSync, existsSync, statSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const root = join(here, '..', '..')
const out = join(here, 'build')
const BYTES_PER_MB = 1024 * 1024

// name: file .node prodotto; tags: build tag Go; ldflagsX: variabile impostata con -X.
const variants = [
  { name: 'sysbasic', pkg: './tests/e2e/addons/sysbasic', tags: [] },
  { name: 'sysbasic-v10', pkg: './tests/e2e/addons/sysbasic', tags: ['napi_go_v10'], ldflagsX: 'main.addonName=sysbasic-v10' },
  { name: 'sysbasic-copy', pkg: './tests/e2e/addons/sysbasic', tags: [], ldflagsX: 'main.addonName=sysbasic-copy' },
]

mkdirSync(out, { recursive: true })
for (const v of variants) {
  const target = join(out, `${v.name}.node`)
  const ldflags = ['-s', '-w', ...(v.ldflagsX ? ['-X', v.ldflagsX] : [])].join(' ')
  const args = ['build', '-buildmode=c-shared', '-buildvcs=false', '-trimpath', `-ldflags=${ldflags}`]
  if (v.tags.length) args.push(`-tags=${v.tags.join(',')}`)
  args.push('-o', target, v.pkg)
  const t0 = Date.now()
  const r = spawnSync('go', args, { cwd: root, stdio: 'inherit', env: { ...process.env, CGO_ENABLED: '1' } })
  if (r.error) {
    console.error(`go non eseguibile: ${r.error.message}`)
    process.exit(1)
  }
  if (r.status !== 0) {
    console.error(`build di ${v.name} fallita (exit ${r.status})`)
    process.exit(r.status ?? 1)
  }
  // c-shared produce anche l'header C delle export: non serve.
  const header = target.replace(/\.node$/, '.h')
  if (existsSync(header)) rmSync(header)
  const mb = (statSync(target).size / BYTES_PER_MB).toFixed(2)
  process.stdout.write(`✓ ${v.name}.node  ${mb} MB  (${Date.now() - t0} ms)\n`)
}
