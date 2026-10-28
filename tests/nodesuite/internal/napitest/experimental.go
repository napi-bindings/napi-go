package napitest

/*
#cgo CFLAGS: -I${SRCDIR}/../../../../sys/include
#cgo linux LDFLAGS: -ldl
#include <stdint.h>
#include <stdlib.h>
#include <js_native_api.h>

#if defined(_WIN32)
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#else
#ifndef _GNU_SOURCE
#define _GNU_SOURCE
#endif
#include <dlfcn.h>
#endif

// Le API sperimentali non sono negli header di sys (node-api-headers 1.9.0): i prototipi sono
// quelli di js_native_api.h di nodejs/node main.
typedef napi_status (*gn_post_finalizer_fn)(napi_env, napi_finalize, void*, void*);
typedef napi_status (*gn_is_sab_fn)(napi_env, napi_value, bool*);
typedef napi_status (*gn_set_prototype_fn)(napi_env, napi_value, napi_value);
typedef napi_status (*gn_create_object_with_properties_fn)(napi_env, napi_value, napi_value*,
                                                           napi_value*, size_t, napi_value*);
typedef napi_status (*gn_create_sab_fn)(napi_env, size_t, void**, napi_value*);

// gn_test_sym cerca una funzione Node-API nel processo host, come sys/loader.c.
static void* gn_test_sym(const char* name) {
#if defined(_WIN32)
  void* p = (void*)GetProcAddress(GetModuleHandleW(NULL), name);
  if (p == NULL) {
    HMODULE m = GetModuleHandleW(L"libnode.dll");
    if (m == NULL) m = GetModuleHandleW(L"node.dll");
    if (m != NULL) p = (void*)GetProcAddress(m, name);
  }
  return p;
#else
  return dlsym(RTLD_DEFAULT, name);
#endif
}

static void* gn_test_ptr(uintptr_t addr) { return (void*)addr; }

// Valore volutamente non valido restituito da TestBadReturnExceptionPending.
static napi_value gn_test_invalid_value(void) { return (napi_value)(uintptr_t)0xFFFFFFFFFull; }

static napi_status gn_test_post_finalizer(void* fn, napi_env env, napi_finalize cb, uintptr_t data,
                                          uintptr_t hint) {
  return ((gn_post_finalizer_fn)fn)(env, cb, (void*)data, (void*)hint);
}

static napi_status gn_test_is_sab(void* fn, napi_env env, napi_value value, bool* result) {
  return ((gn_is_sab_fn)fn)(env, value, result);
}

static napi_status gn_test_set_prototype(void* fn, napi_env env, napi_value object,
                                         napi_value prototype) {
  return ((gn_set_prototype_fn)fn)(env, object, prototype);
}

static napi_status gn_test_create_object_with_properties(void* fn, napi_env env, napi_value proto,
                                                         napi_value* names, napi_value* values,
                                                         size_t count, napi_value* result) {
  return ((gn_create_object_with_properties_fn)fn)(env, proto, names, values, count, result);
}

static napi_status gn_test_create_sab(void* fn, napi_env env, size_t byte_length, void** data,
                                      napi_value* result) {
  return ((gn_create_sab_fn)fn)(env, byte_length, data, result);
}
*/
import "C"

import (
	"sync"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// Pointer equivale al cast C (void*)addr: converte il data di un finalizer o di un wrap, che sys
// passa come uintptr, senza una conversione uintptr → unsafe.Pointer lato Go. addr deve venire
// da memoria C o da una variabile globale Go (che il GC non sposta).
func Pointer(addr uintptr) unsafe.Pointer {
	return C.gn_test_ptr(C.uintptr_t(addr))
}

// InvalidValue restituisce un napi_value volutamente non valido (0xFFFFFFFFF), come
// TestBadReturnExceptionPending di test_function.c.
func InvalidValue() sys.Value {
	return sys.Value(unsafe.Pointer(C.gn_test_invalid_value()))
}

type symbol struct {
	name string
	once sync.Once
	fn   unsafe.Pointer
}

func (s *symbol) lookup() unsafe.Pointer {
	s.once.Do(func() {
		name := C.CString(s.name)
		defer C.free(unsafe.Pointer(name))
		s.fn = C.gn_test_sym(name)
	})
	return s.fn
}

var (
	postFinalizerSym              = &symbol{name: "node_api_post_finalizer"}
	isSharedArrayBufferSym        = &symbol{name: "node_api_is_sharedarraybuffer"}
	setPrototypeSym               = &symbol{name: "node_api_set_prototype"}
	createObjectWithPropertiesSym = &symbol{name: "node_api_create_object_with_properties"}
	createSharedArrayBufferSym    = &symbol{name: "node_api_create_sharedarraybuffer"}
)

// Le funzioni seguenti chiamano API sperimentali di Node-API che sys non espone ancora
// (assenti da node-api-headers 1.9.0), risolte dal processo node come fa sys/loader.c. Se il Node
// in esecuzione non le esporta restituiscono sys.StatusUnavailable, salvo dove indicato.

// PostFinalizer chiama node_api_post_finalizer.
func PostFinalizer(env sys.Env, cb sys.Finalize, data, hint uintptr) sys.Status {
	fn := postFinalizerSym.lookup()
	if fn == nil {
		return sys.StatusUnavailable
	}
	return sys.Status(C.gn_test_post_finalizer(fn, C.napi_env(unsafe.Pointer(env)), C.napi_finalize(cb),
		C.uintptr_t(data), C.uintptr_t(hint)))
}

// IsSharedArrayBuffer chiama node_api_is_sharedarraybuffer.
func IsSharedArrayBuffer(env sys.Env, value sys.Value, result *bool) sys.Status {
	fn := isSharedArrayBufferSym.lookup()
	if fn == nil {
		return sys.StatusUnavailable
	}
	var out C.bool
	st := sys.Status(C.gn_test_is_sab(fn, C.napi_env(unsafe.Pointer(env)), C.napi_value(unsafe.Pointer(value)), &out))
	if st == sys.StatusOK && result != nil {
		*result = bool(out)
	}
	return st
}

// CreateSharedArrayBuffer chiama node_api_create_sharedarraybuffer. data e result sono scritti
// da Node-API, come nell'API C.
func CreateSharedArrayBuffer(env sys.Env, byteLength uint, data *unsafe.Pointer, result *sys.Value) sys.Status {
	fn := createSharedArrayBufferSym.lookup()
	if fn == nil {
		return sys.StatusUnavailable
	}
	return sys.Status(C.gn_test_create_sab(fn, C.napi_env(unsafe.Pointer(env)), C.size_t(byteLength), data,
		(*C.napi_value)(unsafe.Pointer(result))))
}

// SetPrototype chiama node_api_set_prototype. Se il Node in esecuzione non la esporta usa
// Object.setPrototypeOf(object, prototype), che ha lo stesso effetto osservabile negli script.
func SetPrototype(env sys.Env, object, prototype sys.Value) sys.Status {
	if fn := setPrototypeSym.lookup(); fn != nil {
		return sys.Status(C.gn_test_set_prototype(fn, C.napi_env(unsafe.Pointer(env)),
			C.napi_value(unsafe.Pointer(object)), C.napi_value(unsafe.Pointer(prototype))))
	}
	var result sys.Value
	args := [2]sys.Value{object, prototype}
	return callObjectMethod(env, "setPrototypeOf", args[:], &result)
}

// CreateObjectWithProperties chiama node_api_create_object_with_properties. Se il Node in
// esecuzione non la esporta usa Object.create(prototype) e napi_set_property per ogni coppia:
// stesse proprietà proprie (dati, enumerabili, scrivibili, configurabili) e stesso prototipo.
// prototype nil equivale a null, come nell'API.
func CreateObjectWithProperties(env sys.Env, prototype sys.Value, names, values []sys.Value,
	result *sys.Value) sys.Status {
	if len(names) != len(values) {
		return sys.StatusInvalidArg
	}
	if fn := createObjectWithPropertiesSym.lookup(); fn != nil {
		var namesPtr, valuesPtr *C.napi_value
		if len(names) > 0 {
			namesPtr = (*C.napi_value)(unsafe.Pointer(&names[0]))
			valuesPtr = (*C.napi_value)(unsafe.Pointer(&values[0]))
		}
		return sys.Status(C.gn_test_create_object_with_properties(fn, C.napi_env(unsafe.Pointer(env)),
			C.napi_value(unsafe.Pointer(prototype)), namesPtr, valuesPtr, C.size_t(len(names)),
			(*C.napi_value)(unsafe.Pointer(result))))
	}
	if prototype == nil {
		if st := sys.GetNull(env, &prototype); st != sys.StatusOK {
			return st
		}
	}
	var object sys.Value
	args := [1]sys.Value{prototype}
	if st := callObjectMethod(env, "create", args[:], &object); st != sys.StatusOK {
		return st
	}
	for i := range names {
		if st := sys.SetProperty(env, object, names[i], values[i]); st != sys.StatusOK {
			return st
		}
	}
	*result = object
	return sys.StatusOK
}

// callObjectMethod chiama Object[method](...args).
func callObjectMethod(env sys.Env, method string, args []sys.Value, result *sys.Value) sys.Status {
	var global, object, fn sys.Value
	if st := sys.GetGlobal(env, &global); st != sys.StatusOK {
		return st
	}
	if st := sys.GetNamedProperty(env, global, CStr("Object"), &object); st != sys.StatusOK {
		return st
	}
	if st := sys.GetNamedProperty(env, object, CStr(method), &fn); st != sys.StatusOK {
		return st
	}
	return sys.CallFunction(env, object, fn, uint(len(args)), &args[0], result)
}
