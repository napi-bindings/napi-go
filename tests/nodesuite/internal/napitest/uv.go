package napitest

/*
#include <stdint.h>
#include <stdlib.h>

// experimental.go
void* gn_test_sym(const char* name);

// uv.c: trampolini C → Go con le firme delle callback di libuv.
void gn_test_uv_cb(void* handle);
void gn_test_uv_close_cb(void* handle);
void gn_test_uv_work_cb(void* req);
void gn_test_uv_after_work_cb(void* req, int status);

typedef size_t (*gn_uv_size_fn)(int);
typedef int (*gn_uv_init_fn)(void*, void*);
typedef int (*gn_uv_start_fn)(void*, void (*)(void*));
typedef void (*gn_uv_close_fn)(void*, void (*)(void*));
typedef int (*gn_uv_async_init_fn)(void*, void*, void (*)(void*));
typedef int (*gn_uv_async_send_fn)(void*);
typedef int (*gn_uv_queue_work_fn)(void*, void*, void (*)(void*), void (*)(void*, int));

static size_t gn_uv_size(void* fn, int kind) { return ((gn_uv_size_fn)fn)(kind); }
static int gn_uv_init(void* fn, void* loop, void* handle) { return ((gn_uv_init_fn)fn)(loop, handle); }
static int gn_uv_start(void* fn, void* handle) { return ((gn_uv_start_fn)fn)(handle, gn_test_uv_cb); }
static void gn_uv_close(void* fn, void* handle) { ((gn_uv_close_fn)fn)(handle, gn_test_uv_close_cb); }
static int gn_uv_async_init(void* fn, void* loop, void* handle) {
  return ((gn_uv_async_init_fn)fn)(loop, handle, gn_test_uv_cb);
}
static int gn_uv_async_send(void* fn, void* handle) { return ((gn_uv_async_send_fn)fn)(handle); }
static int gn_uv_queue_work(void* fn, void* loop, void* req) {
  return ((gn_uv_queue_work_fn)fn)(loop, req, gn_test_uv_work_cb, gn_test_uv_after_work_cb);
}

// Il primo campo di uv_handle_t e di uv_req_t è "void* data" (UV_HANDLE_FIELDS, UV_REQ_FIELDS):
// parte stabile dell'ABI di libuv 1.x.
static void* gn_uv_get_data(void* p) { return *(void**)p; }
static void gn_uv_set_data(void* p, void* data) { *(void**)p = data; }
*/
import "C"

import (
	"sync"
	"unsafe"
)

// Shim minimo di libuv per i test del gruppo B (test_uv_loop, test_async_cleanup_hook,
// test_callback_scope), che usano il loop di Node.js e non sono sostituibili con Go. Le funzioni
// uv_* si risolvono dal processo node come le API Node-API (node le esporta su tutte le
// piattaforme). Gli handle e le richieste sono memoria C opaca grande uv_handle_size /
// uv_req_size: dell'ABI di libuv si usano solo i valori degli enum e il campo data iniziale.

// UVHandleType e UVReqType sono i valori di uv_handle_type e uv_req_type di uv.h (UV_HANDLE_TYPE_MAP
// e UV_REQ_TYPE_MAP, stabili in libuv 1.x).
type (
	UVHandleType int
	UVReqType    int
)

const (
	UVAsync UVHandleType = 1 // UV_ASYNC
	UVCheck UVHandleType = 2 // UV_CHECK
	UVIdle  UVHandleType = 6 // UV_IDLE

	UVWork UVReqType = 7 // UV_WORK
)

type uvSymbol = symbol

var (
	uvHandleSizeSym = &uvSymbol{name: "uv_handle_size"}
	uvReqSizeSym    = &uvSymbol{name: "uv_req_size"}
	uvCheckInitSym  = &uvSymbol{name: "uv_check_init"}
	uvCheckStartSym = &uvSymbol{name: "uv_check_start"}
	uvIdleInitSym   = &uvSymbol{name: "uv_idle_init"}
	uvIdleStartSym  = &uvSymbol{name: "uv_idle_start"}
	uvCloseSym      = &uvSymbol{name: "uv_close"}
	uvAsyncInitSym  = &uvSymbol{name: "uv_async_init"}
	uvAsyncSendSym  = &uvSymbol{name: "uv_async_send"}
	uvQueueWorkSym  = &uvSymbol{name: "uv_queue_work"}
)

// uvFn restituisce la funzione di libuv; se node non la esporta il test non può proseguire.
func uvFn(s *uvSymbol) unsafe.Pointer {
	fn := s.lookup()
	if fn == nil {
		panic("napitest: libuv function " + s.name + " not exported by this Node.js")
	}
	return fn
}

// Callback Go registrate per handle o richiesta (indirizzo C, stabile).
type uvCallbacks struct {
	cb        func(handle unsafe.Pointer)
	close     func(handle unsafe.Pointer)
	work      func(req unsafe.Pointer)
	afterWork func(req unsafe.Pointer, status int)
}

var (
	uvMu              sync.Mutex
	uvCallbacksByAddr = map[uintptr]*uvCallbacks{}
)

func uvEntry(p unsafe.Pointer) *uvCallbacks {
	uvMu.Lock()
	defer uvMu.Unlock()
	e := uvCallbacksByAddr[uintptr(p)]
	if e == nil {
		e = &uvCallbacks{}
		uvCallbacksByAddr[uintptr(p)] = e
	}
	return e
}

func uvLookup(p unsafe.Pointer) *uvCallbacks {
	uvMu.Lock()
	defer uvMu.Unlock()
	return uvCallbacksByAddr[uintptr(p)]
}

func uvForget(p unsafe.Pointer) {
	uvMu.Lock()
	defer uvMu.Unlock()
	delete(uvCallbacksByAddr, uintptr(p))
}

// NewUVHandle equivale a new uv_<kind>_t: memoria C non inizializzata, da liberare con Free.
func NewUVHandle(kind UVHandleType) unsafe.Pointer {
	return Malloc(uint(C.gn_uv_size(uvFn(uvHandleSizeSym), C.int(kind))))
}

// NewUVReq equivale a malloc(sizeof(uv_<kind>_t)).
func NewUVReq(kind UVReqType) unsafe.Pointer {
	return Malloc(uint(C.gn_uv_size(uvFn(uvReqSizeSym), C.int(kind))))
}

// UVData legge il campo data di un handle o di una richiesta.
func UVData(p unsafe.Pointer) uintptr { return uintptr(C.gn_uv_get_data(p)) }

// SetUVData scrive il campo data di un handle o di una richiesta.
func SetUVData(p unsafe.Pointer, data uintptr) { C.gn_uv_set_data(p, Pointer(data)) }

// UVCheckInit equivale a uv_check_init(loop, check).
func UVCheckInit(loop, check unsafe.Pointer) int {
	return int(C.gn_uv_init(uvFn(uvCheckInitSym), loop, check))
}

// UVCheckStart equivale a uv_check_start(check, cb).
func UVCheckStart(check unsafe.Pointer, cb func(check unsafe.Pointer)) int {
	uvEntry(check).cb = cb
	return int(C.gn_uv_start(uvFn(uvCheckStartSym), check))
}

// UVIdleInit equivale a uv_idle_init(loop, idle).
func UVIdleInit(loop, idle unsafe.Pointer) int {
	return int(C.gn_uv_init(uvFn(uvIdleInitSym), loop, idle))
}

// UVIdleStart equivale a uv_idle_start(idle, cb).
func UVIdleStart(idle unsafe.Pointer, cb func(idle unsafe.Pointer)) int {
	uvEntry(idle).cb = cb
	return int(C.gn_uv_start(uvFn(uvIdleStartSym), idle))
}

// UVAsyncInit equivale a uv_async_init(loop, async, cb).
func UVAsyncInit(loop, async unsafe.Pointer, cb func(async unsafe.Pointer)) int {
	uvEntry(async).cb = cb
	return int(C.gn_uv_async_init(uvFn(uvAsyncInitSym), loop, async))
}

// UVAsyncSend equivale a uv_async_send(async).
func UVAsyncSend(async unsafe.Pointer) int {
	return int(C.gn_uv_async_send(uvFn(uvAsyncSendSym), async))
}

// UVClose equivale a uv_close(handle, closeCb); closeCb può essere nil.
func UVClose(handle unsafe.Pointer, closeCb func(handle unsafe.Pointer)) {
	uvEntry(handle).close = closeCb
	C.gn_uv_close(uvFn(uvCloseSym), handle)
}

// UVQueueWork equivale a uv_queue_work(loop, req, work, afterWork): work gira su un thread del
// pool di libuv, afterWork sul thread del loop.
func UVQueueWork(loop, req unsafe.Pointer, work func(req unsafe.Pointer),
	afterWork func(req unsafe.Pointer, status int)) int {
	e := uvEntry(req)
	e.work, e.afterWork = work, afterWork
	return int(C.gn_uv_queue_work(uvFn(uvQueueWorkSym), loop, req))
}

func dispatchUVCb(handle unsafe.Pointer) {
	if e := uvLookup(handle); e != nil && e.cb != nil {
		e.cb(handle)
	}
}

// dispatchUVCloseCb dimentica l'handle prima della callback, che di solito lo libera.
func dispatchUVCloseCb(handle unsafe.Pointer) {
	e := uvLookup(handle)
	uvForget(handle)
	if e != nil && e.close != nil {
		e.close(handle)
	}
}

func dispatchUVWorkCb(req unsafe.Pointer) {
	if e := uvLookup(req); e != nil && e.work != nil {
		e.work(req)
	}
}

func dispatchUVAfterWorkCb(req unsafe.Pointer, status int) {
	e := uvLookup(req)
	uvForget(req)
	if e != nil && e.afterWork != nil {
		e.afterWork(req, status)
	}
}
