package main

// Questo file contiene le decisioni curate a mano: non si possono dedurre dagli header.
// Ogni nome viene verificato dal generatore: un nome che non esiste negli header è un errore.

// headersVersion è la versione del pacchetto npm node-api-headers vendorizzato in include/.
const headersVersion = "1.9.0"

// excluded sono funzioni dichiarate negli header ma non esposte da sys.
var excluded = map[string]string{
	// Registrazione deprecata: napi-go esporta napi_register_module_v1 (vedi module.c).
	"napi_module_register": "registrazione deprecata, sostituita da napi_register_module_v1",
}

// Classificazione dei parametri puntatore non-const. Per default un `T*` non-const verso un tipo
// scalare è un'uscita singola, restituita per valore dal wrapper C (nessun puntatore Go passa a C).
// Le eccezioni sono array scritti da Node-API o parametri letti e scritti.
const (
	paramInOut = "inout" // letto e scritto: passato come puntatore
	paramArray = "array" // buffer di più elementi: passato come puntatore
)

var paramOverrides = map[string]map[string]string{
	"napi_get_cb_info": {
		"argc": paramInOut, // in: capacità di argv, out: numero reale di argomenti
		"argv": paramArray,
	},
	"napi_get_value_bigint_words": {
		"word_count": paramInOut, // in: capacità di words, out: parole necessarie
		"words":      paramArray,
	},
}

// noCallback: funzioni che non eseguono mai JavaScript e quindi non rientrano mai in Go durante la
// chiamata. Insieme a noescape permettono a cgo di non spostare sullo heap i buffer passati.
// Se una di queste funzioni rientrasse in Go, il runtime termina il processo con un errore
// esplicito: la classificazione è verificata dai test e2e.
//
// Vale solo se Node esegue i finalizer in modo differito (moduli che dichiarano una versione
// Node-API ≤ 10). Con napi_go_experimental i finalizer "basic" possono essere eseguiti in modo
// sincrono durante il GC: in quel caso si usa solo noAlloc.
//
// Escluse di proposito, perché possono eseguire JS (getter, setter, trap dei Proxy, valueOf,
// thenable, microtask, handler di eccezioni): get/set/has/delete di proprietà ed elementi,
// get_property_names, get_all_property_names, get_prototype, coerce_to_*, instanceof,
// define_properties, object_freeze/seal, call_function, new_instance, make_callback, run_script,
// resolve_deferred, reject_deferred, fatal_exception, close_callback_scope.
// Escluse anche set_instance_data (comportamento sul dato precedente non documentato) e
// node_api_create_external_string_* (se Node copia la stringa può chiamare subito il finalizer).
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

// noAlloc: sottoinsieme di noCallback che non alloca sullo heap di V8, quindi non può avviare un GC
// (e finalizer sincroni) nemmeno con napi_go_experimental.
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

// escapes: funzioni che conservano un puntatore ricevuto oltre la durata della chiamata. Non
// ricevono la direttiva noescape. I dati opachi (void* data, hint, context, ...) sono già
// esposti come uintptr e non contano: qui restano solo i buffer.
var escapes = setOf(
	"node_api_create_external_string_latin1", // conserva str finché la stringa JS vive
	"node_api_create_external_string_utf16",
)

// mirrors associa le struct C ai tipi Go specchio scritti a mano in ../../types.go (con controlli
// di layout a tempo di compilazione).
var mirrors = map[string]string{
	"napi_property_descriptor": "PropertyDescriptor",
	"napi_extended_error_info": "ExtendedErrorInfo",
	"napi_type_tag":            "TypeTag",
	"napi_node_version":        "NodeVersion",
}

// enumConfig dà il nome Go di ogni enum e come derivare i nomi delle costanti:
// si toglie strip dal nome C e si antepone prefix.
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

// initialisms: parti dei nomi C da scrivere con una grafia diversa dal semplice Title case.
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
