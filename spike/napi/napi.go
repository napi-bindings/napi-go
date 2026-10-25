// Package napi è il prototipo (Fase 0) del binding Go per Node-API.
//
// Contiene in forma minimale i tre livelli previsti dal piano:
//   - Layer 0: tabella di simboli Node-API risolti a runtime (gonapi.h, loader.c)
//   - Layer 1: Env, Value, errori, callback, scope
//   - concorrenza: dispatcher per env basato su una TSFN e Promise risolte da goroutine
//
// Il codice serve a validare le scelte architetturali, non è l'API definitiva.
package napi

/*
#cgo CFLAGS: -I${SRCDIR}/include -DNAPI_VERSION=8 -O2
#cgo linux LDFLAGS: -ldl

// Funzioni "foglia": non trattengono puntatori (noescape) e non possono eseguire JS
// né rientrare in Go (nocallback). Permettono di tenere sullo stack Go i buffer e gli
// out-parameter passati a C. NON vanno mai applicate a funzioni che possono eseguire
// JS (call_function, get/set_property, coerce_to_*, resolve_deferred, ...).
#cgo noescape gn_get_string_utf8
#cgo nocallback gn_get_string_utf8
#cgo noescape gn_new_string_utf8
#cgo nocallback gn_new_string_utf8
#cgo noescape gn_napi_typeof
#cgo nocallback gn_napi_typeof
#cgo noescape gn_napi_create_object
#cgo nocallback gn_napi_create_object
#cgo noescape gn_napi_get_value_bool
#cgo nocallback gn_napi_get_value_bool
#cgo noescape gn_napi_get_boolean
#cgo nocallback gn_napi_get_boolean
#cgo noescape gn_napi_create_int64
#cgo nocallback gn_napi_create_int64
#cgo noescape gn_napi_get_value_int64
#cgo nocallback gn_napi_get_value_int64
#cgo nocallback gn_new_double
#cgo nocallback gn_get_double
#cgo nocallback gn_new_int32
#cgo nocallback gn_get_int32
#cgo nocallback gn_undefined

#include "gonapi.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"strconv"
	"unsafe"
)

// Env è il contesto Node-API di una singola invocazione. È valido SOLO sul thread
// JavaScript che lo ha fornito e solo per la durata della callback: non va salvato
// né passato a goroutine (equivalente di `napi_env` / `Napi::Env`).
type Env struct{ raw C.napi_env }

// Value è un handle a un valore JavaScript, valido nello handle scope corrente.
// Porta con sé l'env (come Napi::Value di node-addon-api): 2 word, nessuna allocazione.
type Value struct {
	env C.napi_env
	raw C.napi_value
}

// ValueType rispecchia napi_valuetype.
type ValueType int

const (
	Undefined ValueType = C.napi_undefined
	Null      ValueType = C.napi_null
	Boolean   ValueType = C.napi_boolean
	Number    ValueType = C.napi_number
	String    ValueType = C.napi_string
	Symbol    ValueType = C.napi_symbol
	Object    ValueType = C.napi_object
	Function  ValueType = C.napi_function
	External  ValueType = C.napi_external
	BigInt    ValueType = C.napi_bigint
)

// Status rispecchia napi_status.
type Status int

const (
	StatusOK               Status = C.napi_ok
	StatusPendingException Status = C.napi_pending_exception
	StatusClosing          Status = C.napi_closing
)

// Error è un errore Node-API (status != napi_ok) oppure un errore applicativo con codice.
type Error struct {
	Status  Status
	Code    string
	Message string
}

func (e *Error) Error() string {
	if e.Code != "" {
		return e.Code + ": " + e.Message
	}
	return "napi: " + e.Message + " (status " + strconv.Itoa(int(e.Status)) + ")"
}

// ErrPendingException indica che un'eccezione JS è già pendente: va propagata, non rilanciata.
var ErrPendingException = &Error{Status: StatusPendingException, Message: "JavaScript exception pending"}

// Coded è implementata dagli errori che vogliono impostare `error.code` lato JS.
type Coded interface{ JSCode() string }

// check converte uno status in error. Il percorso felice non alloca.
func check(env C.napi_env, s C.napi_status) error {
	if s == C.napi_ok {
		return nil
	}
	if Status(s) == StatusPendingException {
		return ErrPendingException
	}
	msg := "unknown error"
	if m := C.gn_last_error_message(env); m != nil {
		msg = C.GoString(m)
	}
	return &Error{Status: Status(s), Message: msg}
}

func (e Env) value(r C.gn_value_result) (Value, error) {
	if r.s != C.napi_ok {
		return Value{}, check(e.raw, r.s)
	}
	return Value{e.raw, r.v}, nil
}

// ---- costruttori di valori ----

func (e Env) Undefined() Value {
	r := C.gn_undefined(e.raw)
	return Value{e.raw, r.v}
}

func (e Env) Float64(f float64) (Value, error) { return e.value(C.gn_new_double(e.raw, C.double(f))) }
func (e Env) Int32(i int32) (Value, error)     { return e.value(C.gn_new_int32(e.raw, C.int32_t(i))) }

func (e Env) Int64(i int64) (Value, error) {
	var r C.napi_value
	return Value{e.raw, r}, check(e.raw, C.gn_napi_create_int64(e.raw, C.int64_t(i), &r))
}

func (e Env) Bool(b bool) (Value, error) {
	var r C.napi_value
	return Value{e.raw, r}, check(e.raw, C.gn_napi_get_boolean(e.raw, C.bool(b), &r))
}

// String crea una stringa JS senza copie lato Go: i byte vengono passati ptr+len
// e copiati da V8 (equivalente di napi_create_string_utf8 con length esplicita).
func (e Env) String(s string) (Value, error) {
	p := (*C.char)(unsafe.Pointer(unsafe.StringData(s)))
	return e.value(C.gn_new_string_utf8(e.raw, p, C.size_t(len(s))))
}

func (e Env) Object() (Value, error) {
	var r C.napi_value
	return Value{e.raw, r}, check(e.raw, C.gn_napi_create_object(e.raw, &r))
}

func (e Env) Array(n int) (Value, error) {
	var r C.napi_value
	return Value{e.raw, r}, check(e.raw, C.gn_napi_create_array_with_length(e.raw, C.size_t(n), &r))
}

func (e Env) Global() (Value, error) {
	var r C.napi_value
	return Value{e.raw, r}, check(e.raw, C.gn_napi_get_global(e.raw, &r))
}

// NewError crea un oggetto Error JS da un error Go (codice da Coded o *Error).
func (e Env) NewError(err error) (Value, error) {
	msg, code := errorParts(err)
	var r C.napi_value
	s := C.gn_new_error(e.raw,
		(*C.char)(unsafe.Pointer(unsafe.StringData(msg))), C.size_t(len(msg)),
		(*C.char)(unsafe.Pointer(unsafe.StringData(code))), C.size_t(len(code)), &r)
	return Value{e.raw, r}, check(e.raw, s)
}

// Throw lancia err come eccezione JS, a meno che un'eccezione non sia già pendente.
func (e Env) Throw(err error) {
	if errors.Is(err, ErrPendingException) || bool(C.gn_exception_pending(e.raw)) {
		return
	}
	msg, code := errorParts(err)
	C.gn_throw_error(e.raw,
		(*C.char)(unsafe.Pointer(unsafe.StringData(msg))), C.size_t(len(msg)),
		(*C.char)(unsafe.Pointer(unsafe.StringData(code))), C.size_t(len(code)))
}

func errorParts(err error) (msg, code string) {
	var ne *Error
	var c Coded
	switch {
	case errors.As(err, &ne):
		return ne.Message, ne.Code
	case errors.As(err, &c):
		return err.Error(), c.JSCode()
	default:
		return err.Error(), ""
	}
}

// ---- lettura di valori ----

func (v Value) Env() Env     { return Env{v.env} }
func (v Value) IsZero() bool { return v.raw == nil }

func (v Value) Type() (ValueType, error) {
	var t C.napi_valuetype
	err := check(v.env, C.gn_napi_typeof(v.env, v.raw, &t))
	return ValueType(t), err
}

func (v Value) Float64() (float64, error) {
	r := C.gn_get_double(v.env, v.raw)
	return float64(r.v), check(v.env, r.s)
}

func (v Value) Int32() (int32, error) {
	r := C.gn_get_int32(v.env, v.raw)
	return int32(r.v), check(v.env, r.s)
}

func (v Value) Int64() (int64, error) {
	var r C.int64_t
	return int64(r), check(v.env, C.gn_napi_get_value_int64(v.env, v.raw, &r))
}

func (v Value) Bool() (bool, error) {
	var r C.bool
	return bool(r), check(v.env, C.gn_napi_get_value_bool(v.env, v.raw, &r))
}

// GoString copia una stringa JS in una stringa Go. Fino a 255 byte usa un buffer sullo
// stack e un solo attraversamento cgo; l'unica allocazione è la stringa risultante.
func (v Value) GoString() (string, error) {
	var buf [256]byte
	var n C.size_t
	var copied C.int
	s := C.gn_get_string_utf8(v.env, v.raw, (*C.char)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)), &n, &copied)
	if s != C.napi_ok {
		return "", check(v.env, s)
	}
	if copied != 0 {
		return string(buf[:n]), nil
	}
	b := make([]byte, int(n)+1)
	var w C.size_t
	s = C.gn_napi_get_value_string_utf8(v.env, v.raw, (*C.char)(unsafe.Pointer(&b[0])), C.size_t(len(b)), &w)
	if s != C.napi_ok {
		return "", check(v.env, s)
	}
	return unsafe.String(&b[0], int(w)), nil
}

// Set imposta o[key] = val. Può eseguire setter JS.
func (v Value) Set(key string, val Value) error {
	k := C.gn_new_string_utf8(v.env, (*C.char)(unsafe.Pointer(unsafe.StringData(key))), C.size_t(len(key)))
	if k.s != C.napi_ok {
		return check(v.env, k.s)
	}
	return check(v.env, C.gn_napi_set_property(v.env, v.raw, k.v, val.raw))
}

// Get legge o[key]. Può eseguire getter JS.
func (v Value) Get(key string) (Value, error) {
	k := C.gn_new_string_utf8(v.env, (*C.char)(unsafe.Pointer(unsafe.StringData(key))), C.size_t(len(key)))
	if k.s != C.napi_ok {
		return Value{}, check(v.env, k.s)
	}
	var r C.napi_value
	return Value{v.env, r}, check(v.env, C.gn_napi_get_property(v.env, v.raw, k.v, &r))
}

func (v Value) SetIndex(i uint32, val Value) error {
	return check(v.env, C.gn_napi_set_element(v.env, v.raw, C.uint32_t(i), val.raw))
}

// Call invoca v come funzione JS (sincrona, sul thread JS).
func (v Value) Call(this Value, args ...Value) (Value, error) {
	var argv [8]C.napi_value
	raw := argv[:0]
	if len(args) > len(argv) {
		raw = make([]C.napi_value, 0, len(args))
	}
	for _, a := range args {
		raw = append(raw, a.raw)
	}
	var argp *C.napi_value
	if len(raw) > 0 {
		argp = &raw[0]
	}
	var r C.napi_value
	err := check(v.env, C.gn_napi_call_function(v.env, this.raw, v.raw, C.size_t(len(raw)), argp, &r))
	return Value{v.env, r}, err
}

// Scope esegue fn in un nuovo handle scope (equivalente di Napi::HandleScope).
func (e Env) Scope(fn func() error) error {
	var s C.napi_handle_scope
	if err := check(e.raw, C.gn_napi_open_handle_scope(e.raw, &s)); err != nil {
		return err
	}
	defer C.gn_napi_close_handle_scope(e.raw, s)
	return fn()
}

// ---- diagnostica dello spike ----

// NapiVersion restituisce la versione Node-API supportata dal processo host.
func (e Env) NapiVersion() (uint32, error) {
	var r C.uint32_t
	return uint32(r), check(e.raw, C.gn_napi_get_version(e.raw, &r))
}

// NodeVersion restituisce la versione di Node.js che ospita l'addon.
func (e Env) NodeVersion() (string, error) {
	var r *C.napi_node_version
	if err := check(e.raw, C.gn_napi_get_node_version(e.raw, &r)); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d.%d.%d", r.major, r.minor, r.patch), nil
}

// HasSymbol dice se il processo host esporta una funzione Node-API (rilevamento a runtime).
func HasSymbol(name string) bool {
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	return C.gn_has_symbol(cs) != 0
}

// SymbolStats restituisce (simboli obbligatori risolti, simboli opzionali risolti).
func SymbolStats() (required, optional int) {
	return int(C.gn_symbols_required_count()), int(C.gn_symbols_optional_loaded())
}

// ThreadID restituisce l'id del thread OS corrente.
func ThreadID() uint64 { return uint64(C.gn_thread_id()) }
