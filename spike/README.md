# napi-go — Fase 0: spike di fattibilità

Prototipo minimo che valida le scelte architetturali del piano (`claude/napi-go-piano-implementazione.md`
nel Project) prima di scrivere il binding vero. **Non è l'API definitiva.**

## Contenuto

```
spike/
├── napi/                    mini-binding (Layer 0 + Layer 1 + concorrenza)
│   ├── include/             header Node-API (da Node 22)
│   ├── gonapi.h             tabella di simboli (X-macro) + wrapper static inline + helper "batch"
│   ├── loader.c             risoluzione simboli a runtime: dlsym(RTLD_DEFAULT) / GetProcAddress
│   ├── module.c             NAPI_MODULE_INIT → napi_register_module_v1 + node_api_module_get_api_version_v1
│   ├── trampoline.c         trampolino unico C→Go, trampolino tipizzato, call_js del dispatcher, cleanup hook
│   ├── napi.go              Env, Value, errori, stringhe zero-copy, scope
│   ├── callback.go          registro callback (indice, non puntatore Go), recover dei panic
│   ├── module.go            Export, stato per env (worker_threads), cleanup
│   ├── dispatcher.go        una TSFN per env + coda coalescente, Promise da goroutine
│   └── typed.go             percorso tipizzato: Fn1/Fn2 generici, conversioni in C
├── addons/basic             addon di test principale
├── addons/second            secondo addon (secondo runtime Go nello stesso processo)
├── addons/cref              addon C puro: baseline per il benchmark
├── experiments/cbcost       costo di una callback C→Go per tipo di thread
├── scripts/build.mjs        prototipo di `napi-go build`
├── test/*.test.mjs          suite node:test (Q1–Q8)
├── test/bench.mjs           benchmark per chiamata
└── ci/spike.yml             workflow GitHub Actions (da copiare in .github/workflows/)
```

## Come eseguirlo

```bash
cd spike
node scripts/build.mjs                       # Windows senza gcc: set CC=zig cc
node --test --test-reporter=spec test/*.test.mjs
node test/bench.mjs
cd experiments/cbcost && go run .
```

Requisiti: Go ≥ 1.24 con cgo, Node ≥ 20, un compilatore C (gcc/clang, oppure `zig cc` su Windows).

## Risultati

Ambienti provati:
- **Linux** x64 (Ubuntu 24.04, VM 2 core): Go 1.24.7, Node 22.23.4 e 20.20.0, gcc 13
- **Windows** x64 (22 thread logici): Go 1.27.1, Node 24.21.0, `zig cc` 0.16

macOS, Linux arm64 e Alpine/musl sono coperti da `ci/spike.yml`, ancora da eseguire.

| # | Domanda | Esito | Evidenza |
|---|---|---|---|
| Q1 | `c-shared` caricato da `require()` con entry point definito in C | ✅ Linux, Windows | `napi_register_module_v1` e `node_api_module_get_api_version_v1` esportati anche con zig/lld su Windows. Nessuna dipendenza dichiarata dal binario node |
| Q2 | Risoluzione dinamica dei simboli | ✅ Linux, Windows | 42 simboli obbligatori + 3 opzionali (Node-API 9/10) rilevati a runtime; nessuna import library, nessuno stub |
| Q3 | Dispatch, argomenti, panic, errori | ✅ | 12+ funzioni senza il bug `static dataCopy`; più di 8 argomenti; panic → `Error` con `code: GO_PANIC` e processo vivo; codici di errore; rientranza Go→JS→Go; **pulito con `GOEXPERIMENT=cgocheck2`** |
| Q4 | Goroutine → Promise, worker_threads | ✅ | Risoluzione sempre sul thread JS dell'env. 20k Promise concorrenti con coalescenza del dispatcher. 8 worker concorrenti con cleanup degli env; worker terminati con Promise pendenti senza crash |
| Q5 | **Due runtime Go nello stesso processo** | ✅ nei test | Due addon distinti: chiamate sincrone, async, GC e CPU concorrenti, entrambi caricati in 4 worker. Resta uno scenario non supportato ufficialmente da Go: va tenuto sotto osservazione in CI |
| Q6 | Segnali e ciclo di vita del processo | ✅ | Più di 1000 trap WebAssembly (SIGSEGV di V8) mentre Go fa preemption e GC. SIGINT/SIGTERM (Linux). `child_process`. `process.exit` con goroutine in volo. `gc()` forzato. Il processo esce da solo (TSFN unref) e resta vivo finché ci sono Promise pendenti |
| Q7 | Alpine / musl | ⏳ CI | Il `.node` ha `FLAGS: STATIC_TLS` (TLS initial-exec di Go): il `dlopen` con musl è proprio il punto da verificare |
| Q8 | Overhead per chiamata | ✅ misurato | Vedi sotto; due scoperte che cambiano il piano |

Ripetizioni: Linux 5 esecuzioni complete (Node 22 ×3, Node 20, cgocheck2), Windows 4 esecuzioni complete. Zero fallimenti. Dimensione: 1,8–2,1 MB per `.node` con `-ldflags=-s -w`.

### Benchmark (ns per chiamata, migliore di 5 round da 1M)

| Caso | JS | C puro | Go generico | Go tipizzato |
|---|---:|---:|---:|---:|
| **Linux / Node 22** | | | | |
| `noop()` | 0,5 | 12,6 | 120 | — |
| `add(1.5, 2.5)` | 2,6 | 32,6 | 355 | **131** |
| `echo(16 B)` | 2,8 | 82 | 350 | **216** |
| `echo(1 KiB)` | 2,8 | 446 | 1184 | — |
| Promise da goroutine | | | 5,6–6,5 µs | |
| **Windows / Node 24** | | | | |
| `noop()` | 0,4 | n/d | 1421–1913 | — |
| `add(1.5, 2.5)` | 4,5 | n/d | 1404–2888 | 1312–2493 |
| Promise da goroutine | | | 7,9–11,7 µs | |

Allocazioni Go per chiamata: 0 per i numeri; 1 per stringa letta (la stringa Go risultante).

Callback C→Go vuota (`experiments/cbcost`):

| | Linux (Go 1.24) | Windows (Go 1.27) |
|---|---:|---:|
| Go→C (nop) | 49 | 37 |
| C→Go da thread Go | 61 | 113 |
| C→Go da thread creato in C (= main thread di Node) | 87 | **1001** |

### Scoperte che cambiano il piano

1. **Il modello di costo è "1 ingresso + N uscite".** Ogni chiamata Node-API fatta da Go dentro una callback costa ~70 ns (Linux). Con il trampolino tipizzato il C legge gli argomenti primitivi, entra in Go una sola volta e converte il risultato: `add` passa da 355 a 131 ns, cioè ~100 ns sopra il C puro, che è il minimo teorico. **Conseguenza per il Layer 2:** le firme con primitivi (numeri, bool, stringhe) vanno compilate in descrittori convertiti dal C. Già con i generics (`napi.Fn2`) funziona senza codegen; il codegen genererà glue C per firma.
2. **Su Windows ogni callback C→Go costa ~1 µs**, anche con Go 1.27. Il runtime Go mantiene l'M legato ai thread esterni solo su Unix (Go 1.21, `x_cgo_bindm` con distruttore pthread). In `runtime/cgo/gcc_libinit_windows.c` il meccanismo manca ("No pthreads on Windows, these are always zero"), quindi ogni ingresso dal main thread di Node rifà `needm`/`dropm`. **Conseguenze:**
   - documentare il costo;
   - progettare API a grana grossa (batch, async) per i percorsi caldi;
   - valutare un contributo upstream a Go (binding dell'M con `FlsAlloc` e callback di distruzione).
3. Il race detector (TSan) non si inizializza dentro un `.node` caricato da Node. La logica concorrente (dispatcher, registri) va testata con `go test -race` usando una TSFN finta.
4. `-Wl,-z,nodelete` non serve: Go imposta già `NODELETE` sui `c-shared` (ed è comunque fuori dalla allowlist dei flag cgo).
5. `#cgo noescape`/`nocallback` (Go 1.24) funzionano: zero allocazioni per i getter/setter numerici e per i buffer di lettura stringa sullo stack.
