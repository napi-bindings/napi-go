package napitest

/*
#include <stdint.h>
#include <stdlib.h>

// experimental.go
void* gn_test_sym(const char* name);

// uv.c: C → Go trampolines matching libuv callback signatures.
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

// The first field of uv_handle_t and uv_req_t is "void* data" (UV_HANDLE_FIELDS, UV_REQ_FIELDS):
// a stable part of the libuv 1.x ABI.
static void* gn_uv_get_data(void* p) { return *(void**)p; }
static void gn_uv_set_data(void* p, void* data) { *(void**)p = data; }
*/
import "C"

import (
	"sync"
	"unsafe"
)

// Minimal libuv shim for group B tests (test_uv_loop, test_async_cleanup_hook,
// test_callback_scope), which operate on the Node.js event loop and cannot be replaced with pure
// Go. The uv_* functions are resolved from the node process like Node-API functions (node exports
// them on all platforms). Handles and requests are opaque C memory blocks of size uv_handle_size /
// uv_req_size: only enum values and the leading data field are relied on from the libuv ABI.

// UVHandleType and UVReqType are the values of uv_handle_type and uv_req_type from uv.h
// (UV_HANDLE_TYPE_MAP and UV_REQ_TYPE_MAP, stable in libuv 1.x).
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

// uvFn returns the libuv function; if node does not export it the test cannot proceed.
func uvFn(s *uvSymbol) unsafe.Pointer {
	fn := s.lookup()
	if fn == nil {
		panic("napitest: libuv function " + s.name + " not exported by this Node.js")
	}
	return fn
}

// Go callbacks registered per handle or request (by stable C address).
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

// NewUVHandle corresponds to new uv_<kind>_t: uninitialized C memory, to be freed with Free.
func NewUVHandle(kind UVHandleType) unsafe.Pointer {
	return Malloc(uint(C.gn_uv_size(uvFn(uvHandleSizeSym), C.int(kind))))
}

// NewUVReq corresponds to malloc(sizeof(uv_<kind>_t)).
func NewUVReq(kind UVReqType) unsafe.Pointer {
	return Malloc(uint(C.gn_uv_size(uvFn(uvReqSizeSym), C.int(kind))))
}

// UVData reads the data field of a handle or request.
func UVData(p unsafe.Pointer) uintptr { return uintptr(C.gn_uv_get_data(p)) }

// SetUVData writes the data field of a handle or request.
func SetUVData(p unsafe.Pointer, data uintptr) { C.gn_uv_set_data(p, Pointer(data)) }

// UVCheckInit corresponds to uv_check_init(loop, check).
func UVCheckInit(loop, check unsafe.Pointer) int {
	return int(C.gn_uv_init(uvFn(uvCheckInitSym), loop, check))
}

// UVCheckStart corresponds to uv_check_start(check, cb).
func UVCheckStart(check unsafe.Pointer, cb func(check unsafe.Pointer)) int {
	uvEntry(check).cb = cb
	return int(C.gn_uv_start(uvFn(uvCheckStartSym), check))
}

// UVIdleInit corresponds to uv_idle_init(loop, idle).
func UVIdleInit(loop, idle unsafe.Pointer) int {
	return int(C.gn_uv_init(uvFn(uvIdleInitSym), loop, idle))
}

// UVIdleStart corresponds to uv_idle_start(idle, cb).
func UVIdleStart(idle unsafe.Pointer, cb func(idle unsafe.Pointer)) int {
	uvEntry(idle).cb = cb
	return int(C.gn_uv_start(uvFn(uvIdleStartSym), idle))
}

// UVAsyncInit corresponds to uv_async_init(loop, async, cb).
func UVAsyncInit(loop, async unsafe.Pointer, cb func(async unsafe.Pointer)) int {
	uvEntry(async).cb = cb
	return int(C.gn_uv_async_init(uvFn(uvAsyncInitSym), loop, async))
}

// UVAsyncSend corresponds to uv_async_send(async).
func UVAsyncSend(async unsafe.Pointer) int {
	return int(C.gn_uv_async_send(uvFn(uvAsyncSendSym), async))
}

// UVClose corresponds to uv_close(handle, closeCb); closeCb may be nil.
func UVClose(handle unsafe.Pointer, closeCb func(handle unsafe.Pointer)) {
	uvEntry(handle).close = closeCb
	C.gn_uv_close(uvFn(uvCloseSym), handle)
}

// UVQueueWork corresponds to uv_queue_work(loop, req, work, afterWork): work runs on a libuv
// threadpool worker, afterWork on the event loop thread.
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

// dispatchUVCloseCb forgets the handle before invoking the callback, which typically frees it.
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
