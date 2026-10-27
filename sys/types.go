package sys

/*
#cgo CFLAGS: -I${SRCDIR}/include
#cgo linux LDFLAGS: -ldl
#include "zz_api.h"
*/
import "C"

import (
	"strconv"
	"unsafe"
)

// Handle opachi di Node-API. Sono puntatori C: si confrontano con nil e si passano per valore.
type (
	Env                    C.napi_env
	Value                  C.napi_value
	Ref                    C.napi_ref
	HandleScope            C.napi_handle_scope
	EscapableHandleScope   C.napi_escapable_handle_scope
	CallbackInfo           C.napi_callback_info
	Deferred               C.napi_deferred
	CallbackScope          C.napi_callback_scope
	AsyncContext           C.napi_async_context
	AsyncWork              C.napi_async_work
	ThreadsafeFunction     C.napi_threadsafe_function
	AsyncCleanupHookHandle C.napi_async_cleanup_hook_handle
)

// Puntatori a funzione C accettati da Node-API. Go non può crearli: si usano i trampolini di sys
// (CallbackTrampoline, FinalizeTrampoline, ...) oppure funzioni C del proprio pacchetto.
type (
	Callback                 C.napi_callback
	Finalize                 C.napi_finalize
	ThreadsafeFunctionCallJS C.napi_threadsafe_function_call_js
	AsyncExecuteCallback     C.napi_async_execute_callback
	AsyncCompleteCallback    C.napi_async_complete_callback
	CleanupHook              C.napi_cleanup_hook
	AsyncCleanupHook         C.napi_async_cleanup_hook
)

const (
	// AutoLength corrisponde a NAPI_AUTO_LENGTH: la stringa è terminata da NUL.
	AutoLength = ^uint(0)
	// VersionExperimental corrisponde a NAPI_VERSION_EXPERIMENTAL.
	VersionExperimental = 2147483647
)

// PropertyDescriptor ha lo stesso layout di napi_property_descriptor.
// Utf8Name non deve puntare a memoria Go non fissata (runtime.Pinner) quando la struct viene
// passata a DefineProperties o DefineClass.
type PropertyDescriptor struct {
	Utf8Name   *byte
	Name       Value
	Method     Callback
	Getter     Callback
	Setter     Callback
	Value      Value
	Attributes PropertyAttributes
	Data       uintptr
}

// ExtendedErrorInfo ha lo stesso layout di napi_extended_error_info.
type ExtendedErrorInfo struct {
	ErrorMessage    *byte
	EngineReserved  unsafe.Pointer
	EngineErrorCode uint32
	ErrorCode       Status
}

// TypeTag ha lo stesso layout di napi_type_tag.
type TypeTag struct {
	Lower uint64
	Upper uint64
}

// NodeVersion ha lo stesso layout di napi_node_version.
type NodeVersion struct {
	Major   uint32
	Minor   uint32
	Patch   uint32
	Release *byte
}

// Controlli di layout a tempo di compilazione: una differenza dà un indice negativo o fuori
// intervallo. I wrapper generati convertono i puntatori Go ↔ C basandosi su queste uguaglianze.
var _ = [...]struct{}{
	[1]struct{}{}[unsafe.Sizeof(uint(0))-unsafe.Sizeof(C.size_t(0))],
	[1]struct{}{}[unsafe.Sizeof(false)-unsafe.Sizeof(C.bool(false))],
	[1]struct{}{}[unsafe.Sizeof(Status(0))-unsafe.Sizeof(C.napi_status(0))],
	[1]struct{}{}[unsafe.Sizeof(ValueType(0))-unsafe.Sizeof(C.napi_valuetype(0))],
	[1]struct{}{}[unsafe.Sizeof(TypedArrayType(0))-unsafe.Sizeof(C.napi_typedarray_type(0))],
	[1]struct{}{}[unsafe.Sizeof(PropertyAttributes(0))-unsafe.Sizeof(C.napi_property_attributes(0))],

	[1]struct{}{}[unsafe.Sizeof(PropertyDescriptor{})-unsafe.Sizeof(C.napi_property_descriptor{})],
	[1]struct{}{}[unsafe.Offsetof(PropertyDescriptor{}.Utf8Name)-unsafe.Offsetof(C.napi_property_descriptor{}.utf8name)],
	[1]struct{}{}[unsafe.Offsetof(PropertyDescriptor{}.Name)-unsafe.Offsetof(C.napi_property_descriptor{}.name)],
	[1]struct{}{}[unsafe.Offsetof(PropertyDescriptor{}.Method)-unsafe.Offsetof(C.napi_property_descriptor{}.method)],
	[1]struct{}{}[unsafe.Offsetof(PropertyDescriptor{}.Getter)-unsafe.Offsetof(C.napi_property_descriptor{}.getter)],
	[1]struct{}{}[unsafe.Offsetof(PropertyDescriptor{}.Setter)-unsafe.Offsetof(C.napi_property_descriptor{}.setter)],
	[1]struct{}{}[unsafe.Offsetof(PropertyDescriptor{}.Value)-unsafe.Offsetof(C.napi_property_descriptor{}.value)],
	[1]struct{}{}[unsafe.Offsetof(PropertyDescriptor{}.Attributes)-unsafe.Offsetof(C.napi_property_descriptor{}.attributes)],
	[1]struct{}{}[unsafe.Offsetof(PropertyDescriptor{}.Data)-unsafe.Offsetof(C.napi_property_descriptor{}.data)],

	[1]struct{}{}[unsafe.Sizeof(ExtendedErrorInfo{})-unsafe.Sizeof(C.napi_extended_error_info{})],
	[1]struct{}{}[unsafe.Offsetof(ExtendedErrorInfo{}.ErrorMessage)-unsafe.Offsetof(C.napi_extended_error_info{}.error_message)],
	[1]struct{}{}[unsafe.Offsetof(ExtendedErrorInfo{}.EngineReserved)-unsafe.Offsetof(C.napi_extended_error_info{}.engine_reserved)],
	[1]struct{}{}[unsafe.Offsetof(ExtendedErrorInfo{}.EngineErrorCode)-unsafe.Offsetof(C.napi_extended_error_info{}.engine_error_code)],
	[1]struct{}{}[unsafe.Offsetof(ExtendedErrorInfo{}.ErrorCode)-unsafe.Offsetof(C.napi_extended_error_info{}.error_code)],

	[1]struct{}{}[unsafe.Sizeof(TypeTag{})-unsafe.Sizeof(C.napi_type_tag{})],
	[1]struct{}{}[unsafe.Offsetof(TypeTag{}.Lower)-unsafe.Offsetof(C.napi_type_tag{}.lower)],
	[1]struct{}{}[unsafe.Offsetof(TypeTag{}.Upper)-unsafe.Offsetof(C.napi_type_tag{}.upper)],

	[1]struct{}{}[unsafe.Sizeof(NodeVersion{})-unsafe.Sizeof(C.napi_node_version{})],
	[1]struct{}{}[unsafe.Offsetof(NodeVersion{}.Major)-unsafe.Offsetof(C.napi_node_version{}.major)],
	[1]struct{}{}[unsafe.Offsetof(NodeVersion{}.Minor)-unsafe.Offsetof(C.napi_node_version{}.minor)],
	[1]struct{}{}[unsafe.Offsetof(NodeVersion{}.Patch)-unsafe.Offsetof(C.napi_node_version{}.patch)],
	[1]struct{}{}[unsafe.Offsetof(NodeVersion{}.Release)-unsafe.Offsetof(C.napi_node_version{}.release)],
}

// Name restituisce il nome C della funzione, es. "napi_create_int32".
func (f Fn) Name() string {
	if f < 0 || f >= fnCount {
		return "Fn(" + strconv.Itoa(int(f)) + ")"
	}
	return fnInfo[f].name
}

// String restituisce il nome C della funzione.
func (f Fn) String() string { return f.Name() }

// MinVersion restituisce la versione Node-API in cui la funzione è stata introdotta (0 se f non
// è valida).
func (f Fn) MinVersion() int {
	if f < 0 || f >= fnCount {
		return 0
	}
	return int(fnInfo[f].version)
}

// FnByName cerca una funzione per nome C.
func FnByName(name string) (Fn, bool) {
	for i := range fnInfo {
		if fnInfo[i].name == name {
			return Fn(i), true
		}
	}
	return 0, false
}

// Available dice se il processo host esporta la funzione. Prima che Node carichi il modulo
// (ad esempio in go test) restituisce sempre false.
func Available(f Fn) bool {
	return f >= 0 && f < fnCount && C.gn_api[f] != nil
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
