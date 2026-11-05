// Go port of node-addon-api's test binding (test/binding.cc): every Init* function becomes a Go
// function that builds the same exports object, on top of Layer 1 (napi). The scripts in
// ../test are node-addon-api's own, so a passing script means the Go port behaves like the C++
// one. C++-only constructs (overloads, operators, empty handles, std::string_view) collapse into
// the nearest Go equivalent; each file says how.
package main

import "github.com/napi-bindings/napi-go/napi"

func main() {}

// module is one entry of binding.cc's exports: the property name and the function building it.
type module struct {
	name string
	init func(env napi.Env) (napi.Object, error)
}

// modules lists the ported modules in binding.cc order. Modules for asynchronous work, threadsafe
// functions, callback scopes and async contexts belong to Phase 3 and are not ported yet.
var modules = []module{
	{"addon_data", initAddonData},
	{"arraybuffer", initArrayBuffer},
	{"globalObject", initGlobalObject},
	{"basic_types_array", initBasicTypesArray},
	{"basic_types_boolean", initBasicTypesBoolean},
	{"basic_types_number", initBasicTypesNumber},
	{"basic_types_value", initBasicTypesValue},
	{"bigint", initBigInt},
	{"date", initDate},
	{"buffer", initBuffer},
	{"bufferNoExternal", initBufferNoExternal},
	{"dataview", initDataView},
	{"dataview_read_write", initDataViewReadWrite},
	{"env_cleanup", initEnvCleanup},
	{"error", initError},
	{"errorHandlingPrim", initErrorHandlingPrim},
	{"external", initExternal},
	{"function", initFunction},
	{"name", initName},
	{"handlescope", initHandleScope},
	{"memory_management", initMemoryManagement},
	{"object", initObject},
	{"promise", initPromise},
	{"run_script", initRunScript},
	{"symbol", initSymbol},
	{"typedarray", initTypedArray},
	{"objectwrap", initObjectWrap},
	{"objectwrapConstructorException", initObjectWrapConstructorException},
	{"objectwrap_removewrap", initObjectWrapRemoveWrap},
	{"reference", initReference},
	{"version_management", initVersionManagement},
	{"object_freeze_seal", initObjectFreezeSeal},
	{"type_taggable", initTypeTaggable},
	{"env_misc", initEnvMiscellaneous},
}

func init() {
	napi.Init(func(env napi.Env, exports napi.Object) (napi.Value, error) {
		for _, m := range modules {
			o, err := m.init(env)
			if err != nil {
				return napi.Value{}, err
			}
			if err := exports.Set(m.name, o.Value); err != nil {
				return napi.Value{}, err
			}
		}
		// The binding is built for the default Node-API version (8), never the experimental one;
		// SharedArrayBuffer creation is not ported.
		for _, flag := range []string{"isExperimental", "hasSharedArrayBuffer"} {
			f, err := env.NewBoolean(false)
			if err != nil {
				return napi.Value{}, err
			}
			if err := exports.Set(flag, f); err != nil {
				return napi.Value{}, err
			}
		}
		return napi.Value{}, nil
	})
}
