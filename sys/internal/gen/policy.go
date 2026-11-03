package main

// This file contains hand-curated policy decisions that cannot be inferred from the C headers.
// Every function and enum name is validated by the generator: referencing a name absent from the
// headers is an error.

// headersVersion is the version of the npm package node-api-headers vendored in include/.
const headersVersion = "1.9.0"

// excluded lists functions declared in the headers that are intentionally omitted from sys.
var excluded = map[string]string{
	// Deprecated module registration: napi-go exports napi_register_module_v1 (see module.c).
	"napi_module_register": "deprecated registration mechanism, superseded by napi_register_module_v1",
}

// Classification of non-const pointer parameters. By default, a non-const `T*` pointing to a
// scalar type is treated as a single output parameter and returned by value from the C wrapper
// (so no Go pointer crosses cgo). Exceptions are caller-allocated arrays filled by Node-API or
// read-write (in/out) parameters.
const (
	paramInOut = "inout" // read and written: passed as a pointer
	paramArray = "array" // multi-element buffer: passed as a pointer
)

var paramOverrides = map[string]map[string]string{
	"napi_get_cb_info": {
		"argc": paramInOut, // in: capacity of argv, out: actual argument count
		"argv": paramArray,
	},
	"napi_get_value_bigint_words": {
		"word_count": paramInOut, // in: capacity of words, out: required word count
		"words":      paramArray,
	},
}

// noCallback lists functions that never execute JavaScript and therefore never re-enter Go during
// the call. Combined with noescape, this allows cgo to keep passed buffers on the stack.
// If any of these functions were to re-enter Go, the Go runtime would terminate the process with
// an explicit fatal error; this classification is verified by the e2e test suite.
//
// This holds only when Node defers finalizers (modules declaring a Node-API version ≤ 10).
// With napi_go_experimental, "basic" finalizers may run synchronously during garbage collection,
// in which case only noAlloc is marked nocallback.
//
// Intentionally excluded because they may execute JS (getters, setters, Proxy traps, valueOf,
// thenables, microtasks, uncaught-exception handlers): property and element get/set/has/delete,
// get_property_names, get_all_property_names, get_prototype, coerce_to_*, instanceof,
// define_properties, object_freeze/seal, call_function, new_instance, make_callback, run_script,
// resolve_deferred, reject_deferred, fatal_exception, close_callback_scope.
// Also excluded: set_instance_data (behavior when replacing existing instance data is undocumented)
// and node_api_create_external_string_* (Node may invoke the finalizer immediately if it copies
// the string instead of adopting the external buffer).
var noCallback = setOf(
	"napi_get_last_error_info",
	"napi_get_undefined", "napi_get_null", "napi_get_global", "napi_get_boolean",
	"napi_create_object", "napi_create_array", "napi_create_array_with_length",
	"napi_create_double", "napi_create_int32", "napi_create_uint32", "napi_create_int64",
	"napi_create_string_latin1", "napi_create_string_utf8", "napi_create_string_utf16",
	"node_api_create_property_key_latin1", "node_api_create_property_key_utf8",
	"node_api_create_property_key_utf16",
	"napi_create_symbol", "node_api_symbol_for", "napi_create_function",
	"napi_create_error", "napi_create_type_error", "napi_create_range_error",
	"node_api_create_syntax_error",
	"napi_typeof", "napi_get_value_double", "napi_get_value_int32", "napi_get_value_uint32",
	"napi_get_value_int64", "napi_get_value_bool",
	"napi_get_value_string_latin1", "napi_get_value_string_utf8", "napi_get_value_string_utf16",
	"napi_is_array", "napi_get_array_length", "napi_strict_equals",
	"napi_get_cb_info", "napi_get_new_target", "napi_define_class",
	"napi_wrap", "napi_unwrap", "napi_remove_wrap",
	"napi_create_external", "napi_get_value_external",
	"napi_create_reference", "napi_delete_reference", "napi_reference_ref", "napi_reference_unref",
	"napi_get_reference_value",
	"napi_open_handle_scope", "napi_close_handle_scope",
	"napi_open_escapable_handle_scope", "napi_close_escapable_handle_scope", "napi_escape_handle",
	"napi_throw", "napi_throw_error", "napi_throw_type_error", "napi_throw_range_error",
	"node_api_throw_syntax_error",
	"napi_is_error", "napi_is_exception_pending", "napi_get_and_clear_last_exception",
	"napi_is_arraybuffer", "napi_create_arraybuffer", "napi_create_external_arraybuffer",
	"napi_get_arraybuffer_info", "napi_is_typedarray", "napi_create_typedarray",
	"napi_get_typedarray_info", "napi_create_dataview", "napi_is_dataview", "napi_get_dataview_info",
	"napi_get_version", "napi_create_promise", "napi_is_promise",
	"napi_adjust_external_memory",
	"napi_create_date", "napi_is_date", "napi_get_date_value",
	"napi_add_finalizer",
	"napi_create_bigint_int64", "napi_create_bigint_uint64", "napi_create_bigint_words",
	"napi_get_value_bigint_int64", "napi_get_value_bigint_uint64", "napi_get_value_bigint_words",
	"napi_get_instance_data",
	"napi_detach_arraybuffer", "napi_is_detached_arraybuffer",
	"napi_type_tag_object", "napi_check_object_type_tag",
	"napi_fatal_error",
	"napi_async_init", "napi_async_destroy",
	"napi_create_buffer", "napi_create_external_buffer", "node_api_create_buffer_from_arraybuffer",
	"napi_create_buffer_copy", "napi_is_buffer", "napi_get_buffer_info",
	"napi_create_async_work", "napi_delete_async_work", "napi_queue_async_work",
	"napi_cancel_async_work",
	"napi_get_node_version", "napi_get_uv_event_loop",
	"napi_add_env_cleanup_hook", "napi_remove_env_cleanup_hook", "napi_open_callback_scope",
	"napi_create_threadsafe_function", "napi_get_threadsafe_function_context",
	"napi_call_threadsafe_function", "napi_acquire_threadsafe_function",
	"napi_release_threadsafe_function", "napi_unref_threadsafe_function",
	"napi_ref_threadsafe_function",
	"napi_add_async_cleanup_hook", "napi_remove_async_cleanup_hook",
	"node_api_get_module_file_name",
)

// noAlloc is the subset of noCallback that never allocates on the V8 heap, and therefore cannot
// trigger garbage collection (or synchronous finalizers) even under napi_go_experimental.
var noAlloc = setOf(
	"napi_get_last_error_info",
	"napi_get_undefined", "napi_get_null", "napi_get_global", "napi_get_boolean",
	"napi_typeof", "napi_get_value_double", "napi_get_value_int32", "napi_get_value_uint32",
	"napi_get_value_int64", "napi_get_value_bool",
	"napi_get_value_bigint_int64", "napi_get_value_bigint_uint64",
	"napi_is_array", "napi_get_array_length", "napi_strict_equals",
	"napi_get_cb_info", "napi_get_new_target", "napi_get_value_external",
	"napi_is_error", "napi_is_exception_pending",
	"napi_is_arraybuffer", "napi_is_typedarray", "napi_is_dataview", "napi_is_promise",
	"napi_is_date", "napi_get_date_value", "napi_is_buffer", "napi_is_detached_arraybuffer",
	"napi_get_version", "napi_get_node_version", "napi_get_uv_event_loop",
	"napi_get_instance_data", "napi_get_threadsafe_function_context",
	"node_api_get_module_file_name",
)

// escapes lists functions that retain a passed pointer beyond the duration of the call. They do
// not receive a noescape directive. Opaque data parameters (void* data, hint, context, ...) are
// already exposed as uintptr and do not count here: only buffer pointers remain.
var escapes = setOf(
	"node_api_create_external_string_latin1", // retains str for the lifetime of the JS string
	"node_api_create_external_string_utf16",
)

// mirrors maps C struct types to their hand-written mirror Go types in ../../types.go (verified
// by compile-time layout assertions).
var mirrors = map[string]string{
	"napi_property_descriptor": "PropertyDescriptor",
	"napi_extended_error_info": "ExtendedErrorInfo",
	"napi_type_tag":            "TypeTag",
	"napi_node_version":        "NodeVersion",
}

// enumConfig specifies the Go type name for each C enum and how to derive its constant names:
// strip is removed from the C constant name and prefix is prepended.
type enumNaming struct {
	goType string
	strip  string
	prefix string
}

var enumConfig = map[string]enumNaming{
	"napi_status":                           {"Status", "napi_", "Status"},
	"napi_valuetype":                        {"ValueType", "napi_", "Type"},
	"napi_typedarray_type":                  {"TypedArrayType", "napi_", ""},
	"napi_property_attributes":              {"PropertyAttributes", "napi_", "Property"},
	"napi_key_collection_mode":              {"KeyCollectionMode", "napi_key_", "Key"},
	"napi_key_filter":                       {"KeyFilter", "napi_key_", "Key"},
	"napi_key_conversion":                   {"KeyConversion", "napi_key_", "Key"},
	"napi_threadsafe_function_release_mode": {"ThreadsafeFunctionReleaseMode", "napi_tsfn_", "TSFN"},
	"napi_threadsafe_function_call_mode":    {"ThreadsafeFunctionCallMode", "napi_tsfn_", "TSFN"},
}

// initialisms maps snake_case word segments from C names to their idiomatic Go capitalization.
var initialisms = map[string]string{
	"ok":          "OK",
	"js":          "JS",
	"jsproperty":  "JSProperty",
	"uv":          "UV",
	"utf8":        "UTF8",
	"utf16":       "UTF16",
	"latin1":      "Latin1",
	"bigint":      "BigInt",
	"biguint64":   "BigUint64",
	"arraybuffer": "ArrayBuffer",
	"typedarray":  "TypedArray",
	"dataview":    "DataView",
	"tsfn":        "TSFN",
}

func setOf(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}
