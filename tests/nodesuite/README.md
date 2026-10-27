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
| `status.json` | test saltati, sempre (`skip`) o sotto una versione di Node.js (`minNodeMajor`) |
| `<cartella>/napi-go.json` (facoltativo) | defines aggiuntivi di un target quando il C li imposta nel sorgente (es. `#define NAPI_VERSION 9`) |

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
   | `{ "n", NULL, fn, NULL, NULL, NULL, attrs, NULL }` | `nt.Method("n", fn, attrs)` |
   | `{ "n", NULL, NULL, get, set, NULL, attrs, NULL }` | `nt.Accessor("n", get, set, attrs)` |
   | `{ "n", NULL, NULL, NULL, NULL, v, attrs, NULL }` | `nt.Value("n", v, attrs)` |
   | `napi_wrap(…, finalize_cb, hint, …)` | `cb, hint := nt.Finalizer(fn, hint)` e poi `sys.Wrap(…, cb, hint, …)` |
   | `(void*)data` in un finalizer | `nt.Pointer(data)` |
   | `napi_create_async_work(…, Execute, Complete, data, …)` | `sys.CreateAsyncWork(…, sys.AsyncExecuteTrampoline, sys.AsyncCompleteTrampoline, nt.AsyncWorkData(execute, complete, data), …)` |

   Il testo della condizione passato ad `Assert` va copiato dal C così com'è: la macro lo
   inserisce nel messaggio e gli script lo confrontano con espressioni regolari.
3. Le utilità di libuv usate solo come strumento del test (`uv_sleep`, `uv_thread_create`,
   `uv_mutex_*`) si sostituiscono con l'equivalente Go, segnalandolo nel commento del file.

## Differenze inevitabili rispetto al C

- Il `data` delle funzioni JS create da Go trasporta l'indice della funzione registrata in
  `napitest` (dispatch di `sys.CallbackTrampoline`), non il `data` del test C.
- I nomi passati ai descrittori di proprietà sono stringhe C condivise e mai liberate
  (`nt.CStr`, `nt.UTF16`), così i descrittori non contengono puntatori Go.
- Getter e setter di un accessor condividono il `data` (come in Node-API): `nt.Accessor` registra la
  coppia e il dispatch sceglie il getter per una chiamata senza argomenti, il setter per una con un
  argomento. L'indice 0 del registro è riservato, così un `data` NULL non chiama mai una funzione.
- Le funzioni create più volte (factory) si registrano una volta sola, a livello di pacchetto:
  ogni `nt.Register` aggiunge una voce al registro.
- I finalizer passano per `sys.FinalizeTrampoline`: `nt.Finalizer(fn, hint)` restituisce la coppia
  callback/hint da usare al posto di quella del C, e `fn` riceve il `finalize_data` di Node-API e
  l'hint originale. La memoria che Node-API deve poter usare dopo la chiamata (stringhe e
  ArrayBuffer esterni) si alloca con `nt.Malloc` e si libera con `nt.Free` / `nt.FreeAddr`.

## Stato

| Suite | Test portati |
|---|---|
| js-native-api | 2_function_arguments, 3_callbacks, 4_object_factory, 5_function_factory, test_array, test_bigint, test_constructor, test_conversions, test_dataview, test_date, test_error, test_exception, test_function, test_general, test_handle_scope, test_new_target, test_number, test_object, test_promise, test_properties, test_string, test_symbol, test_typedarray |
| node-api | 1_hello_world, test_async |

Gli script vengono da nodejs/node `main`, quindi alcuni usano funzionalità recenti: `status.json`
li salta sotto la versione di Node.js che le introduce (oggi tre script richiedono Node.js ≥ 24).

### API sperimentali

Quattro funzioni usate dai test non sono negli header di `sys` (node-api-headers 1.9.0).
`napitest` le risolve dal processo node, come fa `sys/loader.c`:

| Funzione | Usata da | Se il Node in esecuzione non la esporta |
|---|---|---|
| `node_api_post_finalizer` | test_general/testFinalizer | errore `napitest: Node-API function not exported…` |
| `node_api_is_sharedarraybuffer` | test_dataview | come sopra |
| `node_api_set_prototype` | test_general | `Object.setPrototypeOf` |
| `node_api_create_object_with_properties` | test_object | `Object.create` + `napi_set_property` |

Per le ultime due l'equivalente con API stabili ha lo stesso effetto osservabile negli script; serve
solo a non perdere il resto di test_object e test_general sulle versioni di Node.js che non le
hanno ancora.

La valutazione completa delle due suite (copertura, esclusioni, rischi, piano) è nel documento
di progetto `napi-go-valutazione-js-native-api.md`.
