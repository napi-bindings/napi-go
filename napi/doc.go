// Package napi è il Layer 1 di napi-go: un'API Go idiomatica sopra Node-API (pacchetto sys) per
// scrivere moduli nativi Node.js (.node).
//
//	package main
//
//	import "github.com/napi-bindings/napi-go/napi"
//
//	func init() {
//		napi.Init(func(env napi.Env, exports napi.Object) (napi.Value, error) {
//			hello, err := env.NewFunction("hello", func(c napi.CallContext) (napi.Value, error) {
//				s, err := c.Env().NewString("world")
//				return s.Value, err
//			})
//			if err != nil {
//				return napi.Value{}, err
//			}
//			return napi.Value{}, exports.Set("hello", hello.Value)
//		})
//	}
//
//	func main() {}
//
// Si compila con go build -buildmode=c-shared -o hello.node.
//
// # Valori
//
// Env e Value avvolgono napi_env e napi_value. Value porta con sé il suo env (due word, nessuna
// allocazione) e i tipi specializzati (Object, Array, Function, String, BigInt, Buffer, ...)
// incorporano Value: i metodi di Value valgono per tutti e il campo Value promosso restituisce il
// valore generico (obj.Set("s", str.Value)). Le conversioni controllate sono Value.AsObject,
// Value.AsString, ...
//
// Un Value vale solo sul thread JS del suo env e nello handle scope corrente: per conservarlo oltre
// la callback si usa Value.NewRef. Nei cicli che creano molti valori, Env.WithScope li rilascia
// a ogni iterazione.
//
// # Errori ed eccezioni
//
// Ogni operazione che può fallire restituisce error. Un fallimento di Node-API è un *Error con lo
// Status e il messaggio di Node. Se un'eccezione JS è pendente (JS chiamato da Go ha lanciato) si
// ottiene ErrPendingException: restituirla da una callback lascia propagare l'eccezione;
// Env.Catch la intercetta.
//
// Una Callback che restituisce un errore lo lancia in JS: un *Error con il costruttore di Kind
// (TypeError, RangeError, ...) e il suo code, un errore che implementa Coder come Error con code,
// qualsiasi altro errore come Error con message err.Error(). Throw lancia un valore JS qualsiasi.
// Un panic in una callback diventa un Error JS con code "GO_PANIC".
//
// # Thread
//
// Tutte le funzioni di napi vanno chiamate sul thread JS dell'env, dentro una callback o
// l'inizializzazione del modulo; Env e Value non vanno passati a goroutine. Le API asincrone
// (goroutine che risolvono Promise, threadsafe function tipizzate) sono previste nella Fase 3.
//
// # Valori Go affidati a JS
//
// Le callback, i valori associati con Object.Wrap e Env.NewExternal e i finalizer sono conservati
// in tabelle Go e Node-API riceve solo un id, come vogliono le regole di cgo. Vengono rilasciati
// quando V8 raccoglie l'oggetto JS che li tiene (o l'env viene distrutto). Una funzione creata con
// Env.NewFunction o una classe definita con Env.DefineClass tiene in vita le sue callback finché vive.
//
// # Worker
//
// Le funzioni registrate con Init girano per ogni env che carica l'addon (main thread e
// worker_threads). Ogni env ha il proprio stato: Env.SetInstanceData, Env.AddCleanupHook.
//
// napi registra gli handler di sys (SetModuleInitHandler, SetCallbackHandler, SetFinalizeHandler,
// SetCleanupHookHandler) e usa napi_set_instance_data: un addon che usa napi non deve chiamare le
// funzioni corrispondenti di sys.
package napi
