# Node.js Test Suite Ported to `sys`

The Node.js [`test/js-native-api`](https://github.com/nodejs/node/tree/main/test/js-native-api) and
[`test/node-api`](https://github.com/nodejs/node/tree/main/test/node-api) test suites, with the C
addons rewritten in Go on top of Layer 0 (`sys`). The JS scripts are the original ones,
**unmodified**: if a test passes, the Go addon behaves identically to the C addon, including error
messages.

Source: nodejs/node @ `75191c45afeb`. License of copied files: [LICENSE.nodejs](LICENSE.nodejs) (MIT).

```bash
node tests/nodesuite/run.mjs                    # build and run everything
node tests/nodesuite/run.mjs test_async         # only tests whose id contains "test_async"
node tests/nodesuite/run.mjs --no-build test_number/test_null
GOEXPERIMENT=cgocheck2 node tests/nodesuite/run.mjs
```

## Layout

| Path | Contents |
|---|---|
| `run.mjs` | runner: builds targets from each `binding.gyp`, runs each `.js` honoring its `// Flags:` and `// Env:` lines |
| `common/` | minimal replacement for Node.js's `test/common` (`mustCall`, `buildType`, `nodeProcessAborted`, …); `require-as.js`, `tick.js`, `tmpdir.js`, `report.js`, `fixtures.js`, `child_process.js`, and `sea.js` are copied verbatim |
| `fixtures/` | `test/fixtures` files used by the scripts (`dotenv/uv-threadpool.env`, `sea/addon/`) |
| `internal/napitest/` | Go equivalent of `common.h` / `common-inl.h` |
| `js-native-api/<test>/`, `node-api/<test>/` | original `binding.gyp` and `*.js` + Go port (`main.go`, …) |
| `status.json` | skipped tests, unconditionally (`skip`), below a Node.js version (`minNodeMajor`, or `minNodeVersion` with a minimum per release line), or on specific platforms (`skipPlatforms`, e.g. `darwin-x64`) |
| `<dir>/napi-go.json` (optional) | extra defines for a target when the C source sets them inline (e.g. `#define NAPI_VERSION 9`) |

Runner rules, modeled after Node.js's `test/testpy`:

- each `.js` file in a test directory is a test (id `suite/dir/file`, without extension);
- a test passes if the process exits with code 0; `1..0 # Skipped` on stdout counts it as skipped;
- for each target in `binding.gyp`, the Go source lives in `<dir>/<target>/`, or directly in `<dir>/`
  when there is only one target; the addon is written to `build/Release/<target>.node`;
- when multiple targets compile the same C source (e.g. with different defines), the port lives in
  `<dir>/internal/<name>/` and each `<target>/main.go` only registers its `Init`; `#if NAPI_VERSION`
  differences branch on `sys.DeclaredVersion`;
- target defines select the `sys` build tag: none → Node-API 8, `NAPI_VERSION=9` → `napi_go_v9`,
  `NAPI_VERSION=10` → `napi_go_v10`, `NAPI_EXPERIMENTAL` → `napi_go_experimental`,
  `NAPI_VERSION=2147483646` → `napi_go_test_version_mismatch` (only `test_module_version_mismatch`);
- a test exceeding 120 s is killed along with all child processes (process group on POSIX,
  `taskkill /T` on Windows) and the report includes `ps` output and, on macOS, thread stack traces
  (`sample`);
- scripts run with `UV_THREADPOOL_SIZE` and `NODE_OPTIONS` stripped from the runner environment,
  matching Node.js CI (an `// Env:` line can set them again); `tmpdir` uses
  `tests/nodesuite/.tmp.0`.

## Porting a Test

1. Copy `binding.gyp` and all `.js` files in the directory from Node.js without modifying them.
2. Rewrite each C source file in Go preserving names, call order, and assertions.
   The macros in `common.h` have equivalents in `napitest`:

   | C | Go |
   |---|---|
   | `NODE_API_CALL(env, call)` | `if !nt.Call(env, call) { return nil }` |
   | `NODE_API_CALL_RETURN_VOID(env, call)` | `if !nt.Call(env, call) { return }` |
   | `NODE_API_ASSERT(env, cond, msg)` | `if !nt.Assert(env, cond, "<cond as in C>", msg) { return nil }` |
   | `NODE_API_BASIC_CALL_RETURN_VOID(env, call)` | `if !nt.BasicCall(env, call) { return }` (stderr + `abort()`) |
   | `NODE_API_BASIC_ASSERT_RETURN_VOID(cond, msg)` | `if !nt.BasicAssert(cond, "<cond as in C>", msg) { return }` |
   | `assert(expr)` from `<assert.h>`, `abort()` | `nt.CAssert(cond, "<expr as in C>")`, `nt.Abort()` |
   | `NODE_API_ASSERT_STATUS(env, cond, msg)` | `nt.AssertStatus(env, cond, "<cond as in C>", msg)` |
   | `NODE_API_CHECK_STATUS(call)` | `if st := call; st != sys.StatusOK { return st }` |
   | `DECLARE_NODE_API_PROPERTY(name, fn)` | `nt.DeclareProperty(name, fn)` |
   | `napi_define_properties(env, obj, n, descs)` | `nt.DefineProperties(env, obj, descs)` |
   | `add_returned_status`, `add_last_status` | `nt.AddReturnedStatus`, `nt.AddLastStatus` |
   | `NAPI_MODULE(…, Init)`, `NAPI_MODULE_INIT()` | `func init() { nt.ModuleInit(initModule) }` |
   | `{ "n", NULL, fn, NULL, NULL, NULL, attrs, NULL }` | `nt.Method("n", fn, attrs)` |
   | `{ "n", NULL, NULL, get, set, NULL, attrs, NULL }` | `nt.Accessor("n", get, set, attrs)` |
   | `{ "n", NULL, NULL, NULL, NULL, v, attrs, NULL }` | `nt.Value("n", v, attrs)` |
   | `napi_wrap(…, finalize_cb, hint, …)` | `cb, hint := nt.Finalizer(fn, hint)` then `sys.Wrap(…, cb, hint, …)` |
   | successful `napi_remove_wrap` | `nt.DiscardFinalizer(hint)`: Node-API will no longer invoke the finalizer |
   | `new MyObject` passed to `napi_wrap` | `cgo.NewHandle(obj)`; `Destructor` calls `Value()` and `Delete()` |
   | `(void*)data` in a finalizer | `nt.Pointer(data)` |
   | `napi_create_async_work(…, Execute, Complete, data, …)` | `sys.CreateAsyncWork(…, sys.AsyncExecuteTrampoline, sys.AsyncCompleteTrampoline, nt.AsyncWorkData(execute, complete, data), …)` |
   | `napi_create_threadsafe_function(…, fin_data, fin_cb, context, call_js, …)` | `callJS, fin, ctx := nt.ThreadsafeFunction(callJs, finCb, context)` then `sys.CreateThreadsafeFunction(…, fin_data, fin, ctx, callJS, …)`; `nt.GetThreadsafeFunctionContext` returns the original context |
   | `napi_add_env_cleanup_hook(env, fn, arg)` | `var hook = nt.RegisterCleanupHook(fn)` at package level, then `sys.AddEnvCleanupHook(env, sys.CleanupHookTrampoline, hook.Arg(arg))` (same value for remove) |
   | `uv_thread_create` / `uv_thread_join`, `uv_sleep`, `uv_hrtime` | `nt.Thread` (`Create`, `Join`), `nt.Sleep`, `nt.HRTime` |
   | `napi_add_async_cleanup_hook(env, fn, arg, &h)` | `var hook = nt.RegisterAsyncCleanupHook(fn)`, then `sys.AddAsyncCleanupHook(env, sys.AsyncCleanupHookTrampoline, hook.Arg(arg), &h)` |
   | `uv_check_*`, `uv_idle_*`, `uv_async_*`, `uv_close`, `uv_queue_work` on the Node loop | `nt.UV*` shim (`uv.go`): handles and requests in C memory (`nt.NewUVHandle`, `nt.NewUVReq`), Go callbacks |

   The condition text passed to `Assert` must be copied verbatim from C: the macro embeds it in the
   error message and the JS scripts match it with regular expressions.
3. libuv utilities used solely as test helpers (`uv_sleep`, `uv_thread_create`, `uv_mutex_*`) are
   replaced with their Go equivalents and noted in the file header comment. Functions that operate
   on the Node.js event loop (handles, `uv_queue_work`) go through the `napitest` shim: `uv_*`
   functions are resolved from the node process just like Node-API functions, and only enum values
   and the leading `data` field of handles and requests (stable in libuv 1.x) are relied on from the
   libuv ABI; sizes come from `uv_handle_size` / `uv_req_size`.

## Unavoidable Differences from C

- The `data` of JS functions created from Go carries the index of the function registered in
  `napitest` (`sys.CallbackTrampoline` dispatch), not the C test's `data`.
- Names passed to property descriptors are shared, never-freed C strings (`nt.CStr`, `nt.UTF16`),
  so descriptors contain no Go pointers.
- An accessor's getter and setter share `data` (as in Node-API): `nt.Accessor` registers the pair
  and dispatch selects the getter for a zero-argument call and the setter for a one-argument call.
  Registry index 0 is reserved so a NULL `data` never invokes a function.
- Functions created repeatedly (factories) are registered once at package level: each `nt.Register`
  appends an entry to the registry.
- C++ classes become Go structs and the native pointer passed to `napi_wrap` / `napi_add_finalizer`
  is a `cgo.Handle`. Structs that C allocates with `malloc` and passes as instance data or
  `finalize_data` (containing only C types such as `napi_ref`) stay in C memory (`nt.Malloc`).
  A finalizer canceled by deleting its reference (`6_object_wrap/nested_wrap`) is never called and
  its `nt.Finalizer` handle remains allocated, just like the memory in the C test.
- A TSFN context is a `napitest` token and is never released: Node-API calls `call_js_cb` with a
  NULL env for items remaining in the queue even after `thread_finalize_cb` and does not signal when
  it is done. `thread_finalize_cb` receives the original context as its hint.
- Finalizers go through `sys.FinalizeTrampoline`: `nt.Finalizer(fn, hint)` returns the callback/hint
  pair to pass instead of the C pair, and `fn` receives Node-API's `finalize_data` and the original
  hint. Memory that Node-API must be able to access after the call returns (external strings and
  ArrayBuffers) is allocated with `nt.Malloc` and freed with `nt.Free` / `nt.FreeAddr`. `nt.Malloc`
  never returns NULL for a non-zero size: if allocation fails it aborts the process with a message,
  because the C tests use the result of `malloc` without checking it.

## Status

| Suite | Ported tests |
|---|---|
| js-native-api | all 32: 2_function_arguments, 3_callbacks, 4_object_factory, 5_function_factory, 6_object_wrap, 7_factory_wrap, 8_passing_wrapped, test_array, test_bigint, test_cannot_run_js, test_constructor, test_conversions, test_dataview, test_date, test_error, test_exception, test_finalizer, test_function, test_general, test_handle_scope, test_instance_data, test_new_target, test_number, test_object, test_promise, test_properties, test_reference, test_reference_double_free, test_sharedarraybuffer, test_string, test_symbol, test_typedarray |
| node-api | all 28: 1_hello_world, test_async, test_async_cleanup_hook, test_callback_scope, test_init_order, test_module_version_mismatch, test_null_init, test_uv_loop, test_async_context, test_buffer, test_cleanup_hook, test_env_teardown_gc, test_exception, test_fatal, test_fatal_exception, test_general, test_instance_data, test_make_callback, test_make_callback_recurse, test_reference_by_node_api_version, test_sea_addon, test_threadsafe_function, test_threadsafe_function_abort, test_threadsafe_function_shutdown, test_uv_threadpool_size, test_worker_buffer_callback, test_worker_terminate, test_worker_terminate_finalization |

The scripts come from nodejs/node `main`, so some exercise recent features: `status.json` skips
them below the Node.js version that introduced them. `minNodeMajor` suffices when a feature ships in
a major release; when it is backported to different minors across release lines, `minNodeVersion`
lists the first release in each line (e.g. `22.23.3`, `24.16.0`, `26.1.0`): lines without a
minimum, unless newer than all listed lines, lack the feature. Minimums are derived from
nodejs/node release tags.

On macOS x64 napi-go supports only one Go runtime per process (see known limitations in
`sys/README.md`): scripts that load two addons in the same process
(`js-native-api/test_cannot_run_js`, `node-api/test_reference_by_node_api_version`) are listed in
`skipPlatforms` for `darwin-x64`. On other platforms they run normally.

### Experimental APIs

Six functions used by the tests are not in `sys`'s headers (node-api-headers 1.9.0).
`napitest` resolves them from the node process, just like `sys/loader.c`:

| Function | Used by | If not exported by the running Node |
|---|---|---|
| `node_api_post_finalizer` | test_general/testFinalizer, test_finalizer | error `napitest: Node-API function not exported…` |
| `node_api_is_sharedarraybuffer` | test_dataview, test_sharedarraybuffer | same as above |
| `node_api_create_sharedarraybuffer` | test_sharedarraybuffer | same as above |
| `node_api_create_external_sharedarraybuffer` | node-api/test_buffer | same as above; the env-less finalizer goes through a C trampoline in `napitest` (`noenv.c`) |
| `node_api_set_prototype` | test_general | `Object.setPrototypeOf` |
| `node_api_create_object_with_properties` | test_object | `Object.create` + `napi_set_property` |

For the last two, the fallback using stable APIs has the same observable effect in the scripts; it
exists only to keep the rest of `test_object` and `test_general` running on Node.js versions that do
not export them yet.
