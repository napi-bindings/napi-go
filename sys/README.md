# sys — Layer 0 di napi-go

Binding 1:1 di tutte le funzioni Node-API 1–10 (154 funzioni, header `node-api-headers` 1.9.0),
generato da `internal/gen`. È la base del Layer 1 (`napi`, Fase 2); si usa direttamente solo se
serve l'ABI così com'è.

```go
func init() {
	sys.SetModuleInitHandler(func(env sys.Env, exports sys.Value) sys.Value {
		var fn sys.Value
		sys.CreateFunction(env, name, sys.AutoLength, sys.CallbackTrampoline, 0, &fn)
		sys.SetNamedProperty(env, exports, name, fn)
		return exports
	})
	sys.SetCallbackHandler(func(env sys.Env, info sys.CallbackInfo, data uintptr, this sys.Value, args []sys.Value) sys.Value {
		// data = 0: la funzione registrata sopra
	})
}
```

Esempio completo: `tests/e2e/addons/sysbasic`.

## Struttura

| File | Contenuto |
|---|---|
| `include/` | header Node-API vendorizzati (solo per i tipi) e `symbols.js` |
| `zz_api.h`, `zz_api.c` | *generati*: tabella dei puntatori `gn_api[]` e wrapper C `static inline` |
| `zz_api.go` | *generato*: wrapper Go, enum `Fn`, direttive `#cgo noescape` |
| `zz_enums.go`, `zz_enums_check.go` | *generati*: enum Node-API e verifica dei valori contro il compilatore C |
| `zz_nocallback*.go` | *generati*: direttive `#cgo nocallback`, per build tag |
| `types.go` | handle, puntatori a funzione, struct specchio con controlli di layout |
| `handlers.go` | trampolini e handler degli ingressi C → Go, `//export` |
| `version_*.go` | versione Node-API dichiarata, per build tag |
| `loader.c` | risoluzione delle funzioni dal processo host (`dlsym` / `GetProcAddress`) |
| `module.c` | `NAPI_MODULE_INIT`: caricamento, guardia, registrazione, correzione dei segnali |
| `trampoline.c` | trampolini C (callback, finalizer, TSFN, async work, cleanup hook) |
| `guard.c` | guardia contro un secondo runtime Go (solo macOS x64) |
| `sigfix.c` | ripristino di `SA_RESETHAND` su `node::SignalExit` (macOS) |

Per rigenerare: `go generate ./sys`. La CI verifica con `go run ./internal/gen -check` che i file
generati siano aggiornati. Le decisioni non deducibili dagli header (eccezioni dei parametri,
`noescape`/`nocallback`, nomi Go degli enum) sono in `internal/gen/policy.go`.

## Scelte di design

**Simboli risolti a runtime.** Nessuna import library e nessun simbolo indefinito al link: il
`.node` non dipende da `node.exe`/`node` e si compila con `go build`/`go test` senza Node. Una
funzione assente dal Node che carica il modulo restituisce `StatusUnavailable`; `Available(Fn…)` lo
dice in anticipo. Le funzioni fino alla versione dichiarata sono obbligatorie: se ne manca una il
caricamento fallisce con `ERR_NAPI_GO_MISSING_FUNCTIONS`.

**Uscite per valore.** cgo evita di spostare sullo heap un puntatore passato a C solo se la
funzione ha *sia* `noescape` *sia* `nocallback`. Le funzioni che possono eseguire JS (getter,
Proxy, `call_function`, ...) non possono essere `nocallback`, quindi `&result` finirebbe sullo heap
a ogni chiamata. Per questo ogni wrapper C restituisce le uscite in una struct per valore: la firma
Go resta 1:1 (puntatori, `nil` = `NULL`), ma nessun puntatore Go di uscita attraversa cgo.
Risultato misurato nei test e2e: **0 allocazioni per chiamata**, anche per `GetNamedProperty`.

**Dati opachi come `uintptr`.** I `void*` conservati da Node-API (data, hint, context, ...) sono
`uintptr`: le regole di cgo vietano di affidare a C puntatori Go, e il tipo lo rende esplicito.

**`noescape` / `nocallback`.** Restano utili per i buffer in ingresso (stringhe, array di argomenti).
`nocallback` è assegnata solo alle funzioni che non eseguono mai JS; se una rientrasse in Go, il
runtime termina il processo con un errore esplicito. Con `napi_go_experimental` i finalizer
possono girare durante il GC di V8 (verificato: tutti eseguiti dentro `gc()`), quindi
`nocallback` resta solo sulle funzioni che non allocano. Il costo è misurabile: i buffer sullo
stack passati, ad esempio, a `GetValueStringUTF8` finiscono sullo heap (1 allocazione per
iterazione nella misura dei test, contro 0 con le versioni 8–10).

## Versione Node-API dichiarata

| Build tag | `DeclaredVersion` | Funzioni obbligatorie |
|---|---|---|
| *(nessuno)* | 8 | Node-API 1–8 |
| `napi_go_v9` | 9 | 1–9 |
| `napi_go_v10` | 10 | 1–10 |
| `napi_go_experimental` | `NAPI_VERSION_EXPERIMENTAL` | 1–10 |
| `napi_go_test_version_mismatch` | `NAPI_VERSION_EXPERIMENTAL - 1` | solo test: Node.js rifiuta il modulo |

`node-api-headers` non contiene le API sperimentali: `napi_go_experimental` cambia solo la
versione dichiarata (e con essa il comportamento di Node, ad esempio sui finalizer).
`napi_go_test_version_mismatch` dichiara una versione che nessun Node.js supporta e serve solo a
verificare il rifiuto del modulo (`tests/nodesuite`, test_module_version_mismatch).

Un addon che non registra un `ModuleInitHandler` non viene caricato: `require()` fallisce con
`ERR_NAPI_GO_NO_HANDLER` e il messaggio di Node.js per un modulo senza entry point
("Module has no declared entry point.").

## Limiti noti

- **macOS x64: un solo addon Go per processo.** Su darwin/amd64 tutti i runtime Go tengono `g`
  nello slot TLS fisso `0x30(GS)` (golang/go#23617). `guard.c` rifiuta l'ingresso nel secondo
  runtime con `ERR_NAPI_GO_FOREIGN_RUNTIME` invece di corrompere la memoria. Il runtime rifiutato
  però è già partito e i suoi handler di segnale restano installati: va ancora studiato.
- **musl/Alpine** non è supportato: le release di Go (≤ 1.27) non caricano un `c-shared` su musl.
- **Windows**: ogni ingresso JS → Go costa circa 1 µs (manca `x_cgo_bindm` nel runtime Go).

Dettagli e misure: `spike/README.md` (Fase 0).
