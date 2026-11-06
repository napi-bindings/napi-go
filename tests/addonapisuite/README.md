# node-addon-api Test Suite Ported to `napi`

The [node-addon-api](https://github.com/nodejs/node-addon-api) tests for the parts of the C++ API
that Layer 1 (`napi`) covers, with the C++ test binding (`test/binding.cc` and the files it
includes) rewritten in Go on top of `napi`. The JS scripts are node-addon-api's own: when one
passes, the Go binding behaves like the C++ one, including error messages. The few lines changed
for napi-go are marked with a `// napi-go:` comment and listed below.

Source: nodejs/node-addon-api @ `25c6b21`. License of copied files:
[LICENSE.node-addon-api.md](LICENSE.node-addon-api.md) (MIT).

```bash
node tests/addonapisuite/run.mjs                  # build and run everything
node tests/addonapisuite/run.mjs object/           # only tests whose id contains "object/"
node tests/addonapisuite/run.mjs --no-build error
GOEXPERIMENT=cgocheck2 node tests/addonapisuite/run.mjs
NAPI_VERSION=8 node tests/addonapisuite/run.mjs    # declare Node-API 8 instead of Node's version
```

## Layout

| Path | Contents |
|---|---|
| `run.mjs` | runner: builds `binding/` into `test/build/Release/binding.node`, runs each script in its own process with the flags of node-addon-api's `test/index.js` |
| `binding/` | Go port of the binding: one file per group of C++ files, each saying how C++-only constructs were mapped |
| `test/` | node-addon-api scripts, at their original paths; `common/index.js`, `testUtil.js`, `napi_child.js` and `child_processes/addon_data.js` are the original helpers |
| `index.js` | stand-in for node-addon-api's root `index.js`, read by `test/napi_child.js` |
| `status.json` | skipped scripts with the reason (none at the moment) |

The binding declares the Node-API version of the running Node.js (or `NAPI_VERSION`), as
node-addon-api's `common.gypi` does: the scripts skip the checks for features the version lacks.

## Ported tests (44)

`addon_data`, `array_buffer`, `basic_types/{array,boolean,number,value}`, `bigint`, `buffer`,
`dataview/{dataview,dataview_read_write}`, `date`, `env_cleanup`, `env_misc`, `error`,
`error_handling_for_primitives`, `external`,
`function`, `globalObject/global_object_{delete,get,has_own,set}_property`, `handlescope`,
`memory_management`, `name`, `object/{delete_property,finalizer,get_property,has_own_property,
has_property,object,object_freeze_seal,set_property,subscript_operator}`, `objectwrap`,
`objectwrap_constructor_exception`, `objectwrap_removewrap`, `promise`, `reference`, `run_script`,
`symbol`, `type_taggable`, `typedarray`, `typedarray-bigint`, `version_management`.

## Changes to the scripts

| File | Change | Reason |
|---|---|---|
| `test/common/index.js` | `runTest` and `runTestWithBindingPath` load only `binding.node` | the other three bindings are C++ build variants of the same sources (no C++ exceptions, `Napi::Maybe`, a custom namespace) |
| `test/name.js` | `nullStringShouldThrow` assertion removed | it tests node-addon-api's own check in `String::New(env, (const char*)nullptr)`; a Go string cannot be null |
| `test/objectwrap.js` | the 4 `/Invalid argument/` matches also accept `does not wrap a Go value` | unwrapping a non-wrapped object returns `napi.ErrNotWrapped`, whose message is napi's own |

## Not ported

| node-addon-api tests | Reason |
|---|---|
| `async_*`, `threadsafe_function/*`, `typed_threadsafe_function/*`, `callbackscope`, `async_context`, `objectwrap_worker_thread`, `error_terminating_environment` | asynchronous work, threadsafe functions and async contexts: Phase 3 |
| `addon` | `Napi::Addon<T>`, the C++ class-based module definition: the Go counterpart is Layer 2 (Phase 4) |
| `objectwrap_function` | a class called without `new` acting as a factory (`ObjectWrap::OnCalledAsFunction`): `napi.DefineClass` always throws a TypeError, as JS classes do |
| `function_reference`, `object_reference`, `movable_callbacks`, `thunking_manual`, `callbackInfo`, `value_type_cast`, `maybe`, `object/object_deprecated`, `objectwrap_multiple_inheritance`, `except_all` | C++ API surface without a Go counterpart (reference wrapper classes, move semantics, manual thunks, `CallbackInfo::SetData`, `Napi::Maybe`, deprecated property descriptors, C++ multiple inheritance, C++ exceptions) |
| `finalizer_order`, `shared_array_buffer`, `require_basic_finalizers` | experimental Node-API (`node_api_post_finalizer`, SharedArrayBuffer creation) not exposed by `napi` yet |
| `exports`, `addon_build` | node-addon-api's npm package and its node-gyp build, not the C++ API |
