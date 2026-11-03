package napitest

import (
	"strconv"
	"sync"

	"github.com/napi-bindings/napi-go/sys"
)

// CleanupHookFunc corresponds to napi_cleanup_hook.
type CleanupHookFunc func(arg uintptr)

// CleanupHook identifies a cleanup function registered with RegisterCleanupHook.
type CleanupHook uint32

type cleanupHookKey struct {
	hook CleanupHook
	arg  uintptr
}

var (
	cleanupMu     sync.Mutex
	cleanupHooks  = []CleanupHookFunc{nil} // index 0 reserved
	cleanupTokens = map[cleanupHookKey]uintptr{}
	cleanupKeys   = []cleanupHookKey{{}} // token → pair; token 0 reserved
)

// RegisterCleanupHook makes fn usable as a cleanup hook. It must be called once per function, at
// package level, like Register.
func RegisterCleanupHook(fn CleanupHookFunc) CleanupHook {
	if fn == nil {
		panic("napitest: RegisterCleanupHook(nil)")
	}
	cleanupMu.Lock()
	defer cleanupMu.Unlock()
	cleanupHooks = append(cleanupHooks, fn)
	return CleanupHook(len(cleanupHooks) - 1)
}

// Arg returns the arg to pass to napi_add_env_cleanup_hook / napi_remove_env_cleanup_hook along
// with sys.CleanupHookTrampoline in place of the C (fun, arg) pair. For the same pair it always
// returns the same value, because Node-API identifies a hook to remove by comparing fun and arg.
// Values are never released: there are few of them and they live for the lifetime of the process,
// like C functions.
func (h CleanupHook) Arg(arg uintptr) uintptr {
	key := cleanupHookKey{hook: h, arg: arg}
	cleanupMu.Lock()
	defer cleanupMu.Unlock()
	if token, ok := cleanupTokens[key]; ok {
		return token
	}
	cleanupKeys = append(cleanupKeys, key)
	token := uintptr(len(cleanupKeys) - 1)
	cleanupTokens[key] = token
	return token
}

func dispatchCleanupHook(token uintptr) {
	cleanupMu.Lock()
	if token == 0 || token >= uintptr(len(cleanupKeys)) {
		cleanupMu.Unlock()
		panic("napitest: unknown cleanup hook token " + strconv.FormatUint(uint64(token), 10))
	}
	key := cleanupKeys[token]
	fn := cleanupHooks[key.hook]
	cleanupMu.Unlock()
	fn(key.arg)
}

// AsyncCleanupHookFunc corresponds to napi_async_cleanup_hook.
type AsyncCleanupHookFunc func(handle sys.AsyncCleanupHookHandle, arg uintptr)

// AsyncCleanupHook identifies a function registered with RegisterAsyncCleanupHook.
type AsyncCleanupHook uint32

var (
	asyncCleanupHooks = []AsyncCleanupHookFunc{nil} // index 0 reserved
	asyncCleanupKeys  = []asyncCleanupKey{{}}       // token → pair; token 0 reserved
)

type asyncCleanupKey struct {
	hook AsyncCleanupHook
	arg  uintptr
}

// RegisterAsyncCleanupHook makes fn usable as an async cleanup hook. It must be called once per
// function, at package level.
func RegisterAsyncCleanupHook(fn AsyncCleanupHookFunc) AsyncCleanupHook {
	if fn == nil {
		panic("napitest: RegisterAsyncCleanupHook(nil)")
	}
	cleanupMu.Lock()
	defer cleanupMu.Unlock()
	asyncCleanupHooks = append(asyncCleanupHooks, fn)
	return AsyncCleanupHook(len(asyncCleanupHooks) - 1)
}

// Arg returns the arg to pass to napi_add_async_cleanup_hook along with
// sys.AsyncCleanupHookTrampoline in place of the C (hook, arg) pair. An async cleanup hook is
// removed using its handle, so each call allocates a new token; tokens are never released, just
// like CleanupHook.Arg.
func (h AsyncCleanupHook) Arg(arg uintptr) uintptr {
	cleanupMu.Lock()
	defer cleanupMu.Unlock()
	asyncCleanupKeys = append(asyncCleanupKeys, asyncCleanupKey{hook: h, arg: arg})
	return uintptr(len(asyncCleanupKeys) - 1)
}

func dispatchAsyncCleanupHook(handle sys.AsyncCleanupHookHandle, token uintptr) {
	cleanupMu.Lock()
	if token == 0 || token >= uintptr(len(asyncCleanupKeys)) {
		cleanupMu.Unlock()
		panic("napitest: unknown async cleanup hook token " + strconv.FormatUint(uint64(token), 10))
	}
	key := asyncCleanupKeys[token]
	fn := asyncCleanupHooks[key.hook]
	cleanupMu.Unlock()
	fn(handle, key.arg)
}
