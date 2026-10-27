# Suite di Node.js portata su `sys`

I test di [`test/js-native-api`](https://github.com/nodejs/node/tree/main/test/js-native-api) e
[`test/node-api`](https://github.com/nodejs/node/tree/main/test/node-api) di Node.js, con gli
addon C riscritti in Go sul Layer 0 (`sys`). Gli script JS sono quelli originali, **senza
modifiche**: se un test passa, l'addon Go si comporta come quello C anche nei messaggi d'errore.

Origine: nodejs/node @ `75191c45afeb`. Licenza dei file copiati: [LICENSE.node](LICENSE.node) (MIT).

```bash
node tests/nodesuite/run.mjs                    # compila ed esegue tutto
node tests/nodesuite/run.mjs test_async         # solo i test il cui id contiene "test_async"
node tests/nodesuite/run.mjs --no-build test_number/test_null
GOEXPERIMENT=cgocheck2 node tests/nodesuite/run.mjs
```

## Struttura

| Percorso | Contenuto |
|---|---|
| `run.mjs` | runner: compila i target di ogni `binding.gyp`, esegue ogni `.js` con le sue righe `// Flags:` e `// Env:` |
| `common/` | sostituto minimo di `test/common` di Node.js (`mustCall`, `buildType`, `nodeProcessAborted`, …) |
| `internal/napitest/` | equivalente Go di `common.h` / `common-inl.h` |
| `js-native-api/<test>/`, `node-api/<test>/` | `binding.gyp` e `*.js` originali + porting Go (`main.go`, …) |
| `status.json` (facoltativo) | test saltati: `{"skip": {"<suite>/<cartella>/<file>": "motivo"}}` |

Regole del runner, riprese da `test/testpy` di Node.js:

- ogni file `.js` di una cartella è un test (id `suite/cartella/file`, senza estensione);
- un test passa se il processo esce con codice 0; `1..0 # Skipped` in stdout lo conta come saltato;
- per ogni target del `binding.gyp` il sorgente Go sta in `<cartella>/<target>/`, oppure nella
  cartella stessa quando il target è uno solo; l'addon va in `build/Release/<target>.node`;
- i defines del target scelgono il build tag di `sys`: nessuno → Node-API 8, `NAPI_VERSION=9` →
  `napi_go_v9`, `NAPI_VERSION=10` → `napi_go_v10`, `NAPI_EXPERIMENTAL` → `napi_go_experimental`.

## Come si porta un test

1. Copiare da Node.js `binding.gyp` e tutti i `.js` della cartella, senza modificarli.
2. Riscrivere ogni sorgente C in Go mantenendo nomi, ordine delle chiamate e asserzioni.
   Le macro di `common.h` hanno un equivalente in `napitest`:

   | C | Go |
   |---|---|
   | `NODE_API_CALL(env, call)` | `if !nt.Call(env, call) { return nil }` |
   | `NODE_API_CALL_RETURN_VOID(env, call)` | `if !nt.Call(env, call) { return }` |
   | `NODE_API_ASSERT(env, cond, msg)` | `if !nt.Assert(env, cond, "<cond come nel C>", msg) { return nil }` |
   | `DECLARE_NODE_API_PROPERTY(name, fn)` | `nt.DeclareProperty(name, fn)` |
   | `napi_define_properties(env, obj, n, descs)` | `nt.DefineProperties(env, obj, descs)` |
   | `add_returned_status`, `add_last_status` | `nt.AddReturnedStatus`, `nt.AddLastStatus` |
   | `NAPI_MODULE(…, Init)`, `NAPI_MODULE_INIT()` | `func init() { nt.ModuleInit(initModule) }` |
   | `napi_create_async_work(…, Execute, Complete, data, …)` | `sys.CreateAsyncWork(…, sys.AsyncExecuteTrampoline, sys.AsyncCompleteTrampoline, nt.AsyncWorkData(execute, complete, data), …)` |

   Il testo della condizione passato ad `Assert` va copiato dal C così com'è: la macro lo
   inserisce nel messaggio e gli script lo confrontano con espressioni regolari.
3. Le utilità di libuv usate solo come strumento del test (`uv_sleep`, `uv_thread_create`,
   `uv_mutex_*`) si sostituiscono con l'equivalente Go, segnalandolo nel commento del file.

## Differenze inevitabili rispetto al C

- Il `data` delle funzioni JS create da Go trasporta l'indice della funzione registrata in
  `napitest` (dispatch di `sys.CallbackTrampoline`), non il `data` del test C.
- I nomi passati ai descrittori di proprietà sono stringhe C condivise e mai liberate
  (`nt.CStr`), così i descrittori non contengono puntatori Go.

## Stato

| Suite | Test portati |
|---|---|
| js-native-api | 2_function_arguments, test_array, test_number |
| node-api | 1_hello_world, test_async |

La valutazione completa delle due suite (copertura, esclusioni, rischi, piano) è nel documento
di progetto `napi-go-valutazione-js-native-api.md`.
