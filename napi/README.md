# napi — Layer 1 di napi-go

API Go idiomatica sopra Node-API, costruita sul Layer 0 (`sys`). È l'equivalente di
node-addon-api per C++: valori tipizzati, errori Go che diventano eccezioni JS, callback, classi,
riferimenti e buffer, senza puntatori Go affidati a C.

```go
package main

import "github.com/napi-bindings/napi-go/napi"

func init() {
	napi.Init(func(env napi.Env, exports napi.Object) (napi.Value, error) {
		add, err := env.NewFunction("add", func(c napi.CallContext) (napi.Value, error) {
			a, err := c.Arg(0).Float64()
			if err != nil {
				return napi.Value{}, err // TypeError ERR_NAPI_NUMBER_EXPECTED in JS
			}
			b, err := c.Arg(1).Float64()
			if err != nil {
				return napi.Value{}, err
			}
			return c.Env().NewFloat64(a + b)
		})
		if err != nil {
			return napi.Value{}, err
		}
		return napi.Value{}, exports.Set("add", add.Value)
	})
}

func main() {}
```

```sh
go build -buildmode=c-shared -o add.node .
node -e "console.log(require('./add.node').add(1, 2))"
```

Esempio completo con tutte le parti dell'API: `tests/e2e/addons/napibasic`.

## Contenuto

| File | Contenuto |
|---|---|
| `env.go` | `Env`, `Init`, stato per env (instance data), versioni, `RunScript`, eccezioni pendenti, `ReadStats` |
| `value.go` | `Value`, `Type`, predicati `Is*`, conversioni `As*`, getter dei primitivi, coercizione |
| `primitive.go` | number, boolean, `String` (UTF-8/UTF-16/Latin-1), `Symbol`, `BigInt` (anche `*big.Int`), `Date` |
| `object.go` | `Object` e proprietà (chiave stringa, valore, indice), chiavi, prototipo, freeze/seal, `Array`, `Property` e `DefineProperties` |
| `function.go` | `Callback`, `CallContext`, `Function` (`NewFunction`, `Call`, `New`), tabella delle callback |
| `class.go` | `DefineClass`, `Promise`/`Deferred` sul thread JS |
| `lifecycle.go` | finalizer, `Wrap`/`Unwrap`/`UnwrapAs`, type tag, `External`, `Ref`, handle scope, cleanup hook |
| `buffer.go` | `Buffer`, `ArrayBuffer` (anche esterni, zero-copy), `TypedArray`, `DataView` |
| `errors.go` | `Error`, `ErrPendingException`, `TypeError`/`RangeError`/`SyntaxError`, `Throw`, regole di lancio |
| `table.go` | tabella id → valore Go con lettura lock-free |

## Scelte di design

**Value con il suo env.** `Value` è `{env, napi_value}`: due word passate per valore, come
`Napi::Value`. I tipi specializzati (`Object`, `Function`, `String`, `Buffer`, ...) incorporano
`Value`, quindi hanno tutti i suoi metodi e `x.Value` restituisce il valore generico senza
conversioni. Gli argomenti di `Set`, `Call` e simili sono `Value` e non un'interfaccia: convertire
una struct in interfaccia allocherebbe.

**Errori.** Ogni operazione che può fallire restituisce `error`. Uno status diverso da `napi_ok`
diventa un `*napi.Error` con il messaggio di `napi_get_last_error_info`, copiato subito (Node lo
sovrascrive alla chiamata successiva). In JS:

| Errore restituito da una callback | Lanciato come |
|---|---|
| `*napi.Error` di uno status `*_expected` | `TypeError`, code `ERR_NAPI_<STATUS>` (es. `ERR_NAPI_NUMBER_EXPECTED`) |
| `*napi.Error` di un altro status | `Error`, code `ERR_NAPI_<STATUS>` |
| `napi.TypeError(code, msg)`, `RangeError`, `SyntaxError` | il costruttore corrispondente, con code |
| errore che implementa `JSCode() string` | `Error` con quel code |
| qualsiasi altro `error` | `Error` con message `err.Error()` |
| `napi.Throw(v)` | il valore `v` così com'è |
| `ErrPendingException` | l'eccezione già pendente, invariata |
| panic | `Error` con code `GO_PANIC` (gestito da `sys`) |

Gli errori avvolti (`fmt.Errorf("...: %w", err)`, `errors.Join`) conservano tipo e code. Se
un'eccezione è già pendente, l'errore restituito viene ignorato: in JS arriva la prima eccezione.
`Env.Catch(err)` intercetta l'eccezione segnalata da `ErrPendingException` e la cancella.

**Id invece di puntatori.** Le regole di cgo vietano di affidare a C puntatori Go. Callback,
valori associati con `Wrap` e `NewExternal`, finalizer e cleanup hook stanno in due tabelle
(`callbacks`, `handles`) e Node-API riceve un id come `data`/`hint`. La lettura è lock-free (load
atomici su segmenti di 256 slot); gli id si liberano nel finalizer dell'oggetto JS che li tiene e
vengono riusati. Una funzione creata con `NewFunction` o una classe di `DefineClass` tiene in vita
le sue callback finché V8 non la raccoglie. `ReadStats` espone i contatori (test di perdite).

**Stato per env.** `Init` gira per ogni env che carica l'addon (main thread e `worker_threads`).
Lo stato di napi per un env sta in `napi_set_instance_data` e viene rilasciato alla distruzione
dell'env; `Env.SetInstanceData` vi aggiunge il valore dell'utente. Per questo un addon che usa napi
non deve chiamare `sys.SetInstanceData` né registrare gli handler di `sys`.

**Allocazioni.** Misurate nei test e2e: 0 allocazioni Go per chiamata per una callback,
lettura/scrittura di numeri, `typeof`, lettura di una proprietà con chiave stringa (chiave creata
con `node_api_create_property_key_utf8` se disponibile) e `Function.Call` senza argomenti.
`GoString` legge le stringhe fino a 250 byte in un buffer sullo stack con un solo attraversamento
di cgo: l'unica allocazione è la stringa Go. `Call` e `New` con argomenti allocano il buffer degli
argomenti (le funzioni che eseguono JS non possono essere `nocallback`, quindi cgo lo sposta sullo
heap).

**Buffer zero-copy.** `Bytes`, `NewBuffer`, `NewArrayBuffer` e `TypedArray.Info` restituiscono
`[]byte` sulla memoria di V8, valida finché l'oggetto vive e non viene detached: va copiata prima
di conservarla o passarla a una goroutine. `NewExternalBuffer`/`NewExternalArrayBuffer` passano a
JS memoria Go fissata con `runtime.Pinner` fino al finalizer; dove i buffer esterni non sono
ammessi (`napi_no_external_buffers_allowed`: sandbox di V8, Electron) ripiegano su una copia.

## Thread

Tutto va chiamato sul thread JS dell'env, dentro una callback o `Init`. `Env`, `Value` e `Ref`
non vanno usati da altre goroutine. Le API per lavorare da goroutine (Promise risolte da
goroutine, threadsafe function tipizzate, rilascio delle Ref da altri thread) arrivano con la
Fase 3.

## Limiti attuali

- Con `napi_go_experimental` i finalizer possono girare dentro il GC di V8: le `FinalizeFunc`
  non devono chiamare funzioni che eseguono JS (`node_api_post_finalizer` non è ancora esposto).
- `DefineClass` crea classi base: l'ereditarietà da una classe nativa funziona da JS
  (`class Sub extends Counter`), non tra classi definite in Go.
- Async context, `MakeCallback`, async work e threadsafe function sono disponibili solo in `sys`.
