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
| `common/` | sostituto minimo di `test/common` di Node.js (`mustCall`, `buildType`, `nodeProcessAborted`, …); `require-as.js`, `tick.js`, `tmpdir.js`, `report.js`, `fixtures.js`, `child_process.js` e `sea.js` sono copiati senza modifiche |
| `fixtures/` | i file di `test/fixtures` usati dagli script (`dotenv/uv-threadpool.env`, `sea/addon/`) |
| `internal/napitest/` | equivalente Go di `common.h` / `common-inl.h` |
| `js-native-api/<test>/`, `node-api/<test>/` | `binding.gyp` e `*.js` originali + porting Go (`main.go`, …) |
| `status.json` | test saltati, sempre (`skip`), sotto una versione di Node.js (`minNodeMajor`, oppure `minNodeVersion` con un minimo per linea di release) o su alcune piattaforme (`skipPlatforms`, es. `darwin-x64`) |
| `<cartella>/napi-go.json` (facoltativo) | defines aggiuntivi di un target quando il C li imposta nel sorgente (es. `#define NAPI_VERSION 9`) |

Regole del runner, riprese da `test/testpy` di Node.js:

- ogni file `.js` di una cartella è un test (id `suite/cartella/file`, senza estensione);
- un test passa se il processo esce con codice 0; `1..0 # Skipped` in stdout lo conta come saltato;
- per ogni target del `binding.gyp` il sorgente Go sta in `<cartella>/<target>/`, oppure nella
  cartella stessa quando il target è uno solo; l'addon va in `build/Release/<target>.node`;
- quando più target compilano lo stesso sorgente C (es. con defines diversi) il porting sta in un
  pacchetto `<cartella>/internal/<nome>/` e ogni `<target>/main.go` registra solo il suo `Init`;
  le differenze di `#if NAPI_VERSION` passano per `sys.DeclaredVersion`;
- i defines del target scelgono il build tag di `sys`: nessuno → Node-API 8, `NAPI_VERSION=9` →
  `napi_go_v9`, `NAPI_VERSION=10` → `napi_go_v10`, `NAPI_EXPERIMENTAL` → `napi_go_experimental`,
  `NAPI_VERSION=2147483646` → `napi_go_test_version_mismatch` (solo test_module_version_mismatch);
- un test che supera i 120 s viene terminato con tutti i processi figli (gruppo di processi su
  POSIX, `taskkill /T` su Windows) e il rapporto include `ps` e, su macOS, lo stack dei thread
  (`sample`);
- gli script girano senza `UV_THREADPOOL_SIZE` e `NODE_OPTIONS` dell'ambiente del runner, come
  nella CI di Node.js (una riga `// Env:` le può reimpostare); `tmpdir` usa `tests/nodesuite/.tmp.0`.

## Come si porta un test

1. Copiare da Node.js `binding.gyp` e tutti i `.js` della cartella, senza modificarli.
2. Riscrivere ogni sorgente C in Go mantenendo nomi, ordine delle chiamate e asserzioni.
   Le macro di `common.h` hanno un equivalente in `napitest`:

   | C | Go |
   |---|---|
   | `NODE_API_CALL(env, call)` | `if !nt.Call(env, call) { return nil }` |
   | `NODE_API_CALL_RETURN_VOID(env, call)` | `if !nt.Call(env, call) { return }` |
   | `NODE_API_ASSERT(env, cond, msg)` | `if !nt.Assert(env, cond, "<cond come nel C>", msg) { return nil }` |
   | `NODE_API_BASIC_CALL_RETURN_VOID(env, call)` | `if !nt.BasicCall(env, call) { return }` (stderr + `abort()`) |
   | `NODE_API_BASIC_ASSERT_RETURN_VOID(cond, msg)` | `if !nt.BasicAssert(cond, "<cond come nel C>", msg) { return }` |
   | `assert(expr)` di `<assert.h>`, `abort()` | `nt.CAssert(cond, "<expr come nel C>")`, `nt.Abort()` |
   | `NODE_API_ASSERT_STATUS(env, cond, msg)` | `nt.AssertStatus(env, cond, "<cond come nel C>", msg)` |
   | `NODE_API_CHECK_STATUS(call)` | `if st := call; st != sys.StatusOK { return st }` |
   | `DECLARE_NODE_API_PROPERTY(name, fn)` | `nt.DeclareProperty(name, fn)` |
   | `napi_define_properties(env, obj, n, descs)` | `nt.DefineProperties(env, obj, descs)` |
   | `add_returned_status`, `add_last_status` | `nt.AddReturnedStatus`, `nt.AddLastStatus` |
   | `NAPI_MODULE(…, Init)`, `NAPI_MODULE_INIT()` | `func init() { nt.ModuleInit(initModule) }` |
   | `{ "n", NULL, fn, NULL, NULL, NULL, attrs, NULL }` | `nt.Method("n", fn, attrs)` |
   | `{ "n", NULL, NULL, get, set, NULL, attrs, NULL }` | `nt.Accessor("n", get, set, attrs)` |
   | `{ "n", NULL, NULL, NULL, NULL, v, attrs, NULL }` | `nt.Value("n", v, attrs)` |
   | `napi_wrap(…, finalize_cb, hint, …)` | `cb, hint := nt.Finalizer(fn, hint)` e poi `sys.Wrap(…, cb, hint, …)` |
   | `napi_remove_wrap` riuscita | `nt.DiscardFinalizer(hint)`: Node-API non chiamerà più il finalizer |
   | `new MyObject` passato a `napi_wrap` | `cgo.NewHandle(obj)`; il `Destructor` fa `Value()` e `Delete()` |
   | `(void*)data` in un finalizer | `nt.Pointer(data)` |
   | `napi_create_async_work(…, Execute, Complete, data, …)` | `sys.CreateAsyncWork(…, sys.AsyncExecuteTrampoline, sys.AsyncCompleteTrampoline, nt.AsyncWorkData(execute, complete, data), …)` |
   | `napi_create_threadsafe_function(…, fin_data, fin_cb, context, call_js, …)` | `callJS, fin, ctx := nt.ThreadsafeFunction(callJs, finCb, context)` e poi `sys.CreateThreadsafeFunction(…, fin_data, fin, ctx, callJS, …)`; `nt.GetThreadsafeFunctionContext` restituisce il context originale |
   | `napi_add_env_cleanup_hook(env, fn, arg)` | `var hook = nt.RegisterCleanupHook(fn)` a livello di pacchetto, poi `sys.AddEnvCleanupHook(env, sys.CleanupHookTrampoline, hook.Arg(arg))` (stesso valore per la remove) |
   | `uv_thread_create` / `uv_thread_join`, `uv_sleep`, `uv_hrtime` | `nt.Thread` (`Create`, `Join`), `nt.Sleep`, `nt.HRTime` |
   | `napi_add_async_cleanup_hook(env, fn, arg, &h)` | `var hook = nt.RegisterAsyncCleanupHook(fn)`, poi `sys.AddAsyncCleanupHook(env, sys.AsyncCleanupHookTrampoline, hook.Arg(arg), &h)` |
   | `uv_check_*`, `uv_idle_*`, `uv_async_*`, `uv_close`, `uv_queue_work` sul loop di Node | shim `nt.UV*` (`uv.go`): handle e richieste in memoria C (`nt.NewUVHandle`, `nt.NewUVReq`), callback Go |

   Il testo della condizione passato ad `Assert` va copiato dal C così com'è: la macro lo
   inserisce nel messaggio e gli script lo confrontano con espressioni regolari.
3. Le utilità di libuv usate solo come strumento del test (`uv_sleep`, `uv_thread_create`,
   `uv_mutex_*`) si sostituiscono con l'equivalente Go, segnalandolo nel commento del file. Le
   funzioni che lavorano sul loop di Node.js (handle, `uv_queue_work`) passano per lo shim di
   `napitest`: le funzioni `uv_*` si risolvono dal processo node come le API Node-API, e dell'ABI di
   libuv si usano solo i valori degli enum dei tipi e il campo `data` iniziale di handle e
   richieste (stabili in libuv 1.x); la dimensione arriva da `uv_handle_size` / `uv_req_size`.

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
- Le classi C++ diventano struct Go e il puntatore nativo di `napi_wrap` / `napi_add_finalizer`
  è un `cgo.Handle`. Le struct che il C alloca con `malloc` e passa come instance data o come
  `finalize_data` (con dentro solo tipi C come `napi_ref`) restano in memoria C (`nt.Malloc`).
  Un finalizer annullato cancellando il suo riferimento (6_object_wrap/nested_wrap) non viene
  più chiamato e il suo `nt.Finalizer` resta allocato, come la memoria nel C.
- Il context di una TSFN è un token di `napitest` e non viene mai rilasciato: Node-API chiama
  `call_js_cb` con env NULL per gli elementi rimasti in coda anche dopo il `thread_finalize_cb` e
  non segnala quando ha finito. Il `thread_finalize_cb` riceve il context originale come hint.
- I finalizer passano per `sys.FinalizeTrampoline`: `nt.Finalizer(fn, hint)` restituisce la coppia
  callback/hint da usare al posto di quella del C, e `fn` riceve il `finalize_data` di Node-API e
  l'hint originale. La memoria che Node-API deve poter usare dopo la chiamata (stringhe e
  ArrayBuffer esterni) si alloca con `nt.Malloc` e si libera con `nt.Free` / `nt.FreeAddr`.
  `nt.Malloc` non restituisce mai NULL per una dimensione non nulla: se l'allocazione fallisce
  termina il processo con un messaggio, perché i test C usano il risultato di `malloc` senza
  controllarlo.

## Stato

| Suite | Test portati |
|---|---|
| js-native-api | tutti i 32: 2_function_arguments, 3_callbacks, 4_object_factory, 5_function_factory, 6_object_wrap, 7_factory_wrap, 8_passing_wrapped, test_array, test_bigint, test_cannot_run_js, test_constructor, test_conversions, test_dataview, test_date, test_error, test_exception, test_finalizer, test_function, test_general, test_handle_scope, test_instance_data, test_new_target, test_number, test_object, test_promise, test_properties, test_reference, test_reference_double_free, test_sharedarraybuffer, test_string, test_symbol, test_typedarray |
| node-api | tutti i 28: 1_hello_world, test_async, test_async_cleanup_hook, test_callback_scope, test_init_order, test_module_version_mismatch, test_null_init, test_uv_loop, test_async_context, test_buffer, test_cleanup_hook, test_env_teardown_gc, test_exception, test_fatal, test_fatal_exception, test_general, test_instance_data, test_make_callback, test_make_callback_recurse, test_reference_by_node_api_version, test_sea_addon, test_threadsafe_function, test_threadsafe_function_abort, test_threadsafe_function_shutdown, test_uv_threadpool_size, test_worker_buffer_callback, test_worker_terminate, test_worker_terminate_finalization |

Gli script vengono da nodejs/node `main`, quindi alcuni usano funzionalità recenti: `status.json`
li salta sotto la versione di Node.js che le introduce. `minNodeMajor` basta quando la
funzionalità arriva con una major; quando arriva con minor diverse per ogni linea di release (i
backport) si usa `minNodeVersion`, con il primo rilascio di ogni linea (es. `22.23.3`, `24.16.0`,
`26.1.0`): le linee senza minimo, se non sono più recenti di tutte, non hanno la funzionalità. I
minimi sono ricavati dai tag di release di nodejs/node.

Su macOS x64 napi-go ammette un solo runtime Go per processo (vedi i limiti noti in `sys/README.md`):
gli script che caricano due addon nello stesso processo (js-native-api/test_cannot_run_js,
node-api/test_reference_by_node_api_version) sono
in `skipPlatforms` per `darwin-x64`. Sulle altre piattaforme girano normalmente.

### API sperimentali

Sei funzioni usate dai test non sono negli header di `sys` (node-api-headers 1.9.0).
`napitest` le risolve dal processo node, come fa `sys/loader.c`:

| Funzione | Usata da | Se il Node in esecuzione non la esporta |
|---|---|---|
| `node_api_post_finalizer` | test_general/testFinalizer, test_finalizer | errore `napitest: Node-API function not exported…` |
| `node_api_is_sharedarraybuffer` | test_dataview, test_sharedarraybuffer | come sopra |
| `node_api_create_sharedarraybuffer` | test_sharedarraybuffer | come sopra |
| `node_api_create_external_sharedarraybuffer` | node-api/test_buffer | come sopra; il finalizer senza env passa per un trampolino C di `napitest` (`noenv.c`) |
| `node_api_set_prototype` | test_general | `Object.setPrototypeOf` |
| `node_api_create_object_with_properties` | test_object | `Object.create` + `napi_set_property` |

Per le ultime due l'equivalente con API stabili ha lo stesso effetto osservabile negli script; serve
solo a non perdere il resto di test_object e test_general sulle versioni di Node.js che non le
hanno ancora.

La valutazione completa delle due suite (copertura, esclusioni, rischi, piano) è nel documento
di progetto `napi-go-valutazione-js-native-api.md`.
