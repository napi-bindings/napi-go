// Q1 caricamento, Q2 simboli dinamici, Q3 dispatch/panic/errori, Q4 Promise da goroutine.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { load } from './helpers.mjs'

const b = load('basic')

test('Q1: il .node c-shared si carica con require() ed espone le export', () => {
  assert.equal(b.hello(), 'hello from Go')
  assert.equal(typeof b.add, 'function')
  assert.ok(Object.keys(b).length >= 30)
})

test('Q2: tutti i simboli obbligatori risolti a runtime, opzionali rilevati', () => {
  const i = b.info()
  assert.ok(i.requiredSymbols > 30)
  assert.ok(i.napiVersion >= 8)
  const major = Number(process.versions.node.split('.')[0])
  if (major >= 18) assert.equal(i.hasSymbolFor, true) // node_api_symbol_for: Node-API 9
  assert.equal(i.nodeVersion, process.versions.node)
})

test('Q3: dispatch corretto su molte funzioni (bug "static dataCopy" dei tentativi precedenti)', () => {
  for (let i = 0; i < 12; i++) assert.equal(b[`f${i}`](), i)
})

test('Q3: argomenti oltre il buffer inline (8) e argomenti mancanti', () => {
  const many = Array.from({ length: 20 }, (_, i) => i + 1)
  assert.equal(b.countArgs(...many), 20)
  assert.equal(b.sumArgs(...many), 210)
  assert.equal(b.countArgs(), 0)
  assert.throws(() => b.add(1), /A number was expected/) // argomento mancante = undefined
})

test('Q3: conversioni stringa (ASCII, UTF-8, vuota, > buffer stack)', () => {
  assert.equal(b.echo(''), '')
  assert.equal(b.echo('àèìòù 😀 漢字'), 'àèìòù 😀 漢字')
  const big = 'x'.repeat(100_000) + '😀'
  assert.equal(b.echo(big), big)
  assert.equal(b.concat('a\u0000b', 'c'), 'a\u0000bc') // NUL interni preservati (ptr+len)
})

test('Q3: panic Go → Error JS GO_PANIC, il processo sopravvive', () => {
  assert.throws(() => b.panicNow('x'), { code: 'GO_PANIC', message: 'boom: x' })
  assert.throws(() => b.panicNil(), (e) => e.code === 'GO_PANIC' && /nil map/.test(e.message))
  assert.equal(b.add(1, 2), 3)
})

test('Q3: error Go → eccezione JS con code; status napi → messaggio Node', () => {
  assert.throws(() => b.fail('bad'), { code: 'E_TEST', message: 'bad' })
  assert.throws(() => b.wrongType('s'), /A number was expected/)
})

test('Q3: rientranza Go → JS → Go ed eccezioni JS che attraversano Go', () => {
  assert.equal(b.callMe((x) => b.add(x, 1), 41), 42)
  assert.throws(() => b.callMe(() => { throw new TypeError('da JS') }, 0), { name: 'TypeError', message: 'da JS' })
  assert.deepEqual(b.makeObject(4), { name: 'squares', values: [0, 1, 4, 9] })
})

test('Q4: Promise risolta sul thread JS, lavoro su thread Go', async () => {
  const r = await b.sleep(10)
  assert.equal(r.ms, 10)
  assert.equal(r.resolveThread, r.jsThread)
  assert.notEqual(r.workThread, r.jsThread)
})

test('Q4: reject e panic asincroni', async () => {
  await assert.rejects(b.rejectAsync('nope'), { code: 'E_ASYNC', message: 'nope' })
  await assert.rejects(b.panicAsync(), { code: 'GO_PANIC', message: 'async boom' })
})

test('Q4: 20.000 Promise concorrenti da goroutine', async () => {
  const N = 20_000
  const runsBefore = b.threadInfo().dispatchRuns
  const res = await Promise.all(Array.from({ length: N }, (_, i) => b.sleep(i % 5)))
  assert.equal(res.length, N)
  assert.ok(res.every((r) => r.resolveThread === r.jsThread))
  // coalescenza: molte risoluzioni per singola esecuzione del dispatcher
  assert.ok(b.threadInfo().dispatchRuns - runsBefore < N)
})

test('Q4: lavoro parallelo su più goroutine', async () => {
  const s = await b.parallelSum(8, 1_000_000)
  assert.equal(typeof s, 'number')
})

test('Q4: chiamate JS ripetute da goroutine via dispatcher', async () => {
  const seen = []
  globalThis.__napiGoTick = (i) => seen.push(i)
  const errs = await b.tick(1000)
  // le post arrivano prima della risoluzione: stesso dispatcher, ordine FIFO
  assert.equal(errs, 0)
  assert.equal(seen.length, 1000)
  assert.deepEqual(seen.slice(0, 5), [0, 1, 2, 3, 4])
  delete globalThis.__napiGoTick
})

test('Q8: percorso tipizzato — conversioni in C, casi limite ed errori', () => {
  assert.equal(b.addTyped(1.5, 2), 3.5)
  assert.ok(Object.is(b.addTyped(-0, -0), -0))
  assert.equal(b.addTyped(Number.MAX_VALUE, Number.MAX_VALUE), Infinity)
  assert.equal(b.echoTyped(''), '')
  assert.equal(b.echoTyped('àé😀\u0000z'), 'àé😀\u0000z')
  const long = 'y'.repeat(5000) // oltre il buffer C (512): percorso lento in ingresso e uscita
  assert.equal(b.echoTyped(long), long)
  assert.equal(b.repeatTyped('ab', 0), '')
  assert.equal(b.repeatTyped('x', 1000), 'x'.repeat(1000))
  assert.equal(b.notTyped(false), true)
  assert.throws(() => b.addTyped('a', 1), { name: 'TypeError', message: 'A number was expected' })
  assert.throws(() => b.addTyped(1), { name: 'TypeError' }) // argomento mancante
  assert.throws(() => b.echoTyped(null), { name: 'TypeError', message: 'A string was expected' })
  assert.throws(() => b.notTyped(1), { name: 'TypeError', message: 'A boolean was expected' })
  assert.throws(() => b.repeatTyped('a', -1), { code: 'E_RANGE' })
  assert.throws(() => b.panicTyped(1), { code: 'GO_PANIC', message: 'typed boom' })
})
