package napitest

import (
	"strconv"
	"sync"

	"github.com/napi-bindings/napi-go/sys"
)

// CleanupHookFunc equivale a napi_cleanup_hook.
type CleanupHookFunc func(arg uintptr)

// CleanupHook identifica una funzione di cleanup registrata con RegisterCleanupHook.
type CleanupHook uint32

type cleanupHookKey struct {
	hook CleanupHook
	arg  uintptr
}

var (
	cleanupMu     sync.Mutex
	cleanupHooks  = []CleanupHookFunc{nil} // indice 0 riservato
	cleanupTokens = map[cleanupHookKey]uintptr{}
	cleanupKeys   = []cleanupHookKey{{}} // token → coppia; token 0 riservato
)

// RegisterCleanupHook rende fn utilizzabile come cleanup hook. Va chiamata una volta per
// funzione, a livello di pacchetto, come Register.
func RegisterCleanupHook(fn CleanupHookFunc) CleanupHook {
	if fn == nil {
		panic("napitest: RegisterCleanupHook(nil)")
	}
	cleanupMu.Lock()
	defer cleanupMu.Unlock()
	cleanupHooks = append(cleanupHooks, fn)
	return CleanupHook(len(cleanupHooks) - 1)
}

// Arg restituisce l'arg da passare a napi_add_env_cleanup_hook / napi_remove_env_cleanup_hook
// insieme a sys.CleanupHookTrampoline al posto della coppia (fun, arg) del C. Per la stessa
// coppia restituisce sempre lo stesso valore, perché Node-API identifica un hook da rimuovere
// confrontando fun e arg. I valori non vengono mai rilasciati: sono pochi e vivono quanto il
// processo, come le funzioni C.
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

// AsyncCleanupHookFunc equivale a napi_async_cleanup_hook.
type AsyncCleanupHookFunc func(handle sys.AsyncCleanupHookHandle, arg uintptr)

// AsyncCleanupHook identifica una funzione registrata con RegisterAsyncCleanupHook.
type AsyncCleanupHook uint32

var (
	asyncCleanupHooks = []AsyncCleanupHookFunc{nil} // indice 0 riservato
	asyncCleanupKeys  = []asyncCleanupKey{{}}       // token → coppia; token 0 riservato
)

type asyncCleanupKey struct {
	hook AsyncCleanupHook
	arg  uintptr
}

// RegisterAsyncCleanupHook rende fn utilizzabile come async cleanup hook. Va chiamata una volta
// per funzione, a livello di pacchetto.
func RegisterAsyncCleanupHook(fn AsyncCleanupHookFunc) AsyncCleanupHook {
	if fn == nil {
		panic("napitest: RegisterAsyncCleanupHook(nil)")
	}
	cleanupMu.Lock()
	defer cleanupMu.Unlock()
	asyncCleanupHooks = append(asyncCleanupHooks, fn)
	return AsyncCleanupHook(len(asyncCleanupHooks) - 1)
}

// Arg restituisce l'arg da passare a napi_add_async_cleanup_hook insieme a
// sys.AsyncCleanupHookTrampoline al posto della coppia (hook, arg) del C. Un async cleanup hook
// si rimuove con il suo handle, quindi ogni chiamata crea un valore nuovo; i valori non vengono
// rilasciati, come per CleanupHook.Arg.
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
