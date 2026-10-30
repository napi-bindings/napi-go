// Addon di test end-to-end del Layer 1 (pacchetto napi): ogni export esercita una parte dell'API
// e i casi limite sono verificati da tests/e2e/napi.test.mjs.
package main

import (
	"errors"
	"math"
	"runtime"
	"sync/atomic"

	"github.com/napi-bindings/napi-go/napi"
)

func main() {}

// Contatori condivisi da tutti gli env del processo.
var (
	wrapFinalized     atomic.Int64
	externalFinalized atomic.Int64
	bufferFinalized   atomic.Int64
	cleanupHooksRun   atomic.Int64
	instanceFinalized atomic.Int64
	inits             atomic.Int64
)

// envData è lo stato per env dell'addon, salvato come instance data.
type envData struct {
	refs   map[uint32]napi.Ref
	nextID uint32
	value  float64
}

// counterTag riconosce gli oggetti Counter creati da questo addon.
var counterTag = napi.TypeTag{Lower: 0x6e6170692d676f31, Upper: 0x636f756e74657221}

type counter struct{ n float64 }

const (
	// byteModulus: i buffer di prova contengono i % byteModulus.
	byteModulus = 251
	// answerValue è il valore delle proprietà answer e [symbol] di defineProps.
	answerValue = 42
	// removedHookMarker è ciò che aggiungerebbe a cleanupHooksRun l'hook rimosso: un valore
	// diverso da 1 rende visibile nel test un'esecuzione indebita.
	removedHookMarker = 1000
)

func init() {
	napi.Init(func(env napi.Env, exports napi.Object) (napi.Value, error) {
		inits.Add(1)
		data := &envData{refs: map[uint32]napi.Ref{}}
		if err := env.SetInstanceData(data, func(napi.Env, any) { instanceFinalized.Add(1) }); err != nil {
			return napi.Value{}, err
		}
		for _, f := range functions {
			fn, err := env.NewFunction(f.name, f.fn)
			if err != nil {
				return napi.Value{}, err
			}
			if err := exports.Set(f.name, fn.Value); err != nil {
				return napi.Value{}, err
			}
		}
		cls, err := defineCounter(env)
		if err != nil {
			return napi.Value{}, err
		}
		return napi.Value{}, exports.Set("Counter", cls.Value)
	})
	// Una seconda funzione di init riceve le export della prima.
	napi.Init(func(env napi.Env, exports napi.Object) (napi.Value, error) {
		has, err := exports.Has("hello")
		if err != nil {
			return napi.Value{}, err
		}
		b, err := env.NewBoolean(has)
		if err != nil {
			return napi.Value{}, err
		}
		return napi.Value{}, exports.Set("secondInitSawExports", b)
	})
}

type export struct {
	name string
	fn   napi.Callback
}

var functions = []export{
	{"hello", func(c napi.CallContext) (napi.Value, error) {
		s, err := c.Env().NewString("world")
		return s.Value, err
	}},
	{"echo", func(c napi.CallContext) (napi.Value, error) { return c.Arg(0), nil }},
	{"argInfo", argInfo},
	{"typeOf", typeOf},
	{"add", func(c napi.CallContext) (napi.Value, error) {
		a, err := c.Arg(0).Float64()
		if err != nil {
			return napi.Value{}, err
		}
		b, err := c.Arg(1).Float64()
		if err != nil {
			return napi.Value{}, err
		}
		return c.Env().NewFloat64(a + b)
	}},
	{"numbers", numbers},
	{"bool", func(c napi.CallContext) (napi.Value, error) {
		b, err := c.Arg(0).Bool()
		if err != nil {
			return napi.Value{}, err
		}
		return c.Env().NewBoolean(!b)
	}},
	{"str", func(c napi.CallContext) (napi.Value, error) {
		s, err := c.Arg(0).GoString()
		if err != nil {
			return napi.Value{}, err
		}
		r, err := c.Env().NewString(s)
		return r.Value, err
	}},
	{"strBytes", func(c napi.CallContext) (napi.Value, error) {
		s, err := c.Arg(0).GoString()
		if err != nil {
			return napi.Value{}, err
		}
		return c.Env().NewInt64(int64(len(s)))
	}},
	{"utf16", func(c napi.CallContext) (napi.Value, error) {
		u, err := c.Arg(0).UTF16()
		if err != nil {
			return napi.Value{}, err
		}
		r, err := c.Env().NewStringUTF16(u)
		return r.Value, err
	}},
	{"latin1", func(c napi.CallContext) (napi.Value, error) {
		b, err := c.Arg(0).Latin1()
		if err != nil {
			return napi.Value{}, err
		}
		r, err := c.Env().NewStringLatin1(b)
		return r.Value, err
	}},
	{"symbol", func(c napi.CallContext) (napi.Value, error) {
		d, err := c.Arg(0).GoString()
		if err != nil {
			return napi.Value{}, err
		}
		if c.Len() > 1 {
			s, err := c.Env().SymbolFor(d)
			return s.Value, err
		}
		s, err := c.Env().NewSymbol(d)
		return s.Value, err
	}},
	{"bigint", bigint},
	{"date", date},
	{"objectOps", objectOps},
	{"keys", keys},
	{"freeze", func(c napi.CallContext) (napi.Value, error) {
		o, err := c.Arg(0).AsObject()
		if err != nil {
			return napi.Value{}, err
		}
		if s, _ := c.Arg(1).Bool(); s {
			return o.Value, o.Seal()
		}
		return o.Value, o.Freeze()
	}},
	{"array", func(c napi.CallContext) (napi.Value, error) {
		a, err := c.Env().NewArrayWithLength(c.Len())
		if err != nil {
			return napi.Value{}, err
		}
		for i := 0; i < c.Len(); i++ {
			if err := a.SetIndex(uint32(i), c.Arg(i)); err != nil {
				return napi.Value{}, err
			}
		}
		return a.Value, nil
	}},
	{"arrayLen", func(c napi.CallContext) (napi.Value, error) {
		a, err := c.Arg(0).AsArray()
		if err != nil {
			return napi.Value{}, err
		}
		n, err := a.Len()
		if err != nil {
			return napi.Value{}, err
		}
		return c.Env().NewUint32(n)
	}},
	{"call", func(c napi.CallContext) (napi.Value, error) {
		fn, err := c.Arg(0).AsFunction()
		if err != nil {
			return napi.Value{}, err
		}
		var rest []napi.Value
		if c.Len() > 2 {
			rest = c.Args(nil)[2:]
		}
		return fn.Call(c.Arg(1), rest...)
	}},
	{"construct", func(c napi.CallContext) (napi.Value, error) {
		fn, err := c.Arg(0).AsFunction()
		if err != nil {
			return napi.Value{}, err
		}
		args := c.Args(nil)
		o, err := fn.New(args[1:]...)
		return o.Value, err
	}},
	{"catch", catchJS},
	{"makeFunction", makeFunction},
	{"throwError", throwError},
	{"coerce", coerce},
	{"compare", compare},
	{"defineProps", defineProps},
	{"wrap", wrap},
	{"external", external},
	{"refs", refs},
	{"buffers", buffers},
	{"typed", typed},
	{"promise", promise},
	{"instanceData", instanceData},
	{"cleanupHook", func(c napi.CallContext) (napi.Value, error) {
		h, err := c.Env().AddCleanupHook(func() { cleanupHooksRun.Add(1) })
		if err != nil {
			return napi.Value{}, err
		}
		// Un secondo hook, rimosso subito, non deve girare.
		removed, err := c.Env().AddCleanupHook(func() { cleanupHooksRun.Add(removedHookMarker) })
		if err != nil {
			return napi.Value{}, err
		}
		if err := removed.Remove(); err != nil {
			return napi.Value{}, err
		}
		// Rimuovere due volte non è un errore.
		if err := removed.Remove(); err != nil {
			return napi.Value{}, err
		}
		_ = h
		return napi.Value{}, nil
	}},
	{"scopes", scopes},
	{"runScript", func(c napi.CallContext) (napi.Value, error) {
		src, err := c.Arg(0).GoString()
		if err != nil {
			return napi.Value{}, err
		}
		return c.Env().RunScript(src)
	}},
	{"versions", versions},
	{"counters", counters},
	{"goMallocs", func(c napi.CallContext) (napi.Value, error) {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		return c.Env().NewFloat64(float64(ms.Mallocs))
	}},
	{"noop", func(c napi.CallContext) (napi.Value, error) { return napi.Value{}, nil }},
	{"hotPath", hotPath},
	// callBack(fn, withArg) chiama fn senza argomenti o con un argomento (misura delle allocazioni).
	{"callBack", func(c napi.CallContext) (napi.Value, error) {
		fn, err := c.Arg(0).AsFunction()
		if err != nil {
			return napi.Value{}, err
		}
		if withArg, _ := c.Arg(1).Bool(); withArg {
			return fn.Call(napi.Value{}, c.Arg(1))
		}
		return fn.Call(napi.Value{})
	}},
	{"panic", func(c napi.CallContext) (napi.Value, error) { panic("boom") }},
}

func argInfo(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	o, err := env.NewObject()
	if err != nil {
		return napi.Value{}, err
	}
	n, err := env.NewInt32(int32(c.Len()))
	if err != nil {
		return napi.Value{}, err
	}
	if err := o.Set("len", n); err != nil {
		return napi.Value{}, err
	}
	if err := o.Set("this", c.This().Value); err != nil {
		return napi.Value{}, err
	}
	// Argomento oltre quelli passati: undefined.
	if err := o.Set("missing", c.Arg(c.Len()+1)); err != nil {
		return napi.Value{}, err
	}
	if err := o.Set("negative", c.Arg(-1)); err != nil {
		return napi.Value{}, err
	}
	last := c.Arg(c.Len() - 1)
	if c.Len() == 0 {
		last = env.Null()
	}
	if err := o.Set("last", last); err != nil {
		return napi.Value{}, err
	}
	nt, err := env.NewBoolean(c.IsConstructCall())
	if err != nil {
		return napi.Value{}, err
	}
	return o.Value, o.Set("isConstruct", nt)
}

var typeNames = map[napi.ValueType]string{
	napi.TypeUndefined: "undefined", napi.TypeNull: "null", napi.TypeBoolean: "boolean",
	napi.TypeNumber: "number", napi.TypeString: "string", napi.TypeSymbol: "symbol",
	napi.TypeObject: "object", napi.TypeFunction: "function", napi.TypeExternal: "external",
	napi.TypeBigInt: "bigint",
}

func typeOf(c napi.CallContext) (napi.Value, error) {
	v := c.Arg(0)
	t, err := v.Type()
	if err != nil {
		return napi.Value{}, err
	}
	env := c.Env()
	o, err := env.NewObject()
	if err != nil {
		return napi.Value{}, err
	}
	name, err := env.NewString(typeNames[t])
	if err != nil {
		return napi.Value{}, err
	}
	if err := o.Set("type", name.Value); err != nil {
		return napi.Value{}, err
	}
	checks := []struct {
		name string
		ok   bool
	}{
		{"isObject", v.IsObject()}, {"isNullish", v.IsNullish()}, {"isArray", v.IsArray()},
		{"isArrayBuffer", v.IsArrayBuffer()}, {"isBuffer", v.IsBuffer()},
		{"isTypedArray", v.IsTypedArray()}, {"isDataView", v.IsDataView()}, {"isDate", v.IsDate()},
		{"isPromise", v.IsPromise()}, {"isError", v.IsError()},
	}
	for _, ch := range checks {
		b, err := env.NewBoolean(ch.ok)
		if err != nil {
			return napi.Value{}, err
		}
		if err := o.Set(ch.name, b); err != nil {
			return napi.Value{}, err
		}
	}
	return o.Value, nil
}

// numbers(v) legge v con ogni getter numerico e restituisce le letture ricreate come number.
func numbers(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	v := c.Arg(0)
	o, err := env.NewObject()
	if err != nil {
		return napi.Value{}, err
	}
	f, err := v.Float64()
	if err != nil {
		return napi.Value{}, err
	}
	i32, err := v.Int32()
	if err != nil {
		return napi.Value{}, err
	}
	u32, err := v.Uint32()
	if err != nil {
		return napi.Value{}, err
	}
	i64, err := v.Int64()
	if err != nil {
		return napi.Value{}, err
	}
	vals := []struct {
		name string
		make func() (napi.Value, error)
	}{
		{"float64", func() (napi.Value, error) { return env.NewFloat64(f) }},
		{"int32", func() (napi.Value, error) { return env.NewInt32(i32) }},
		{"uint32", func() (napi.Value, error) { return env.NewUint32(u32) }},
		{"int64", func() (napi.Value, error) { return env.NewInt64(i64) }},
		// int64 come bigint: senza perdita di precisione oltre 2^53.
		{"int64big", func() (napi.Value, error) { b, err := env.NewBigIntInt64(i64); return b.Value, err }},
	}
	for _, x := range vals {
		n, err := x.make()
		if err != nil {
			return napi.Value{}, err
		}
		if err := o.Set(x.name, n); err != nil {
			return napi.Value{}, err
		}
	}
	return o.Value, nil
}

// bigint(v, mode): "i64" e "u64" restituiscono [valore, lossless]; "big" rilegge con le parole
// e ricrea il bigint; "words" restituisce [negativo, numero di parole].
func bigint(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	b, err := c.Arg(0).AsBigInt()
	if err != nil {
		return napi.Value{}, err
	}
	mode, err := c.Arg(1).GoString()
	if err != nil {
		return napi.Value{}, err
	}
	pair := func(v napi.Value, flag bool) (napi.Value, error) {
		a, err := env.NewArray()
		if err != nil {
			return napi.Value{}, err
		}
		fv, err := env.NewBoolean(flag)
		if err != nil {
			return napi.Value{}, err
		}
		if err := a.SetIndex(0, v); err != nil {
			return napi.Value{}, err
		}
		return a.Value, a.SetIndex(1, fv)
	}
	switch mode {
	case "i64":
		v, lossless, err := b.Int64()
		if err != nil {
			return napi.Value{}, err
		}
		r, err := env.NewBigIntInt64(v)
		if err != nil {
			return napi.Value{}, err
		}
		return pair(r.Value, lossless)
	case "u64":
		v, lossless, err := b.Uint64()
		if err != nil {
			return napi.Value{}, err
		}
		r, err := env.NewBigIntUint64(v)
		if err != nil {
			return napi.Value{}, err
		}
		return pair(r.Value, lossless)
	case "big":
		big, err := b.Big()
		if err != nil {
			return napi.Value{}, err
		}
		r, err := env.NewBigInt(big)
		return r.Value, err
	case "words":
		neg, words, err := b.Words()
		if err != nil {
			return napi.Value{}, err
		}
		n, err := env.NewInt32(int32(len(words)))
		if err != nil {
			return napi.Value{}, err
		}
		return pair(n, neg)
	}
	return napi.Value{}, napi.RangeError("", "unknown mode "+mode)
}

// date(v): rilegge la Date come time.Time e la ricrea; date(ms, true) crea una Date da ms.
func date(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	if fromMillis, _ := c.Arg(1).Bool(); fromMillis {
		ms, err := c.Arg(0).Float64()
		if err != nil {
			return napi.Value{}, err
		}
		d, err := env.NewDateMillis(ms)
		return d.Value, err
	}
	d, err := c.Arg(0).AsDate()
	if err != nil {
		return napi.Value{}, err
	}
	t, err := d.Time()
	if err != nil {
		return napi.Value{}, err
	}
	r, err := env.NewDate(t)
	return r.Value, err
}

// objectOps(o, op, key, value) applica un'operazione sulle proprietà con chiave stringa
// ("get", "set", "has", "hasOwn", "delete"), chiave valore ("getV", "setV", "hasV", "hasOwnV",
// "deleteV") o indice ("getI", "setI", "hasI", "deleteI").
func objectOps(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	o, err := c.Arg(0).AsObject()
	if err != nil {
		return napi.Value{}, err
	}
	op, err := c.Arg(1).GoString()
	if err != nil {
		return napi.Value{}, err
	}
	key := c.Arg(2)
	val := c.Arg(3)
	boolResult := func(b bool, err error) (napi.Value, error) {
		if err != nil {
			return napi.Value{}, err
		}
		return env.NewBoolean(b)
	}
	switch op {
	case "getV":
		return o.GetProperty(key)
	case "setV":
		return napi.Value{}, o.SetProperty(key, val)
	case "hasV":
		return boolResult(o.HasProperty(key))
	case "hasOwnV":
		return boolResult(o.HasOwnProperty(key))
	case "deleteV":
		return boolResult(o.DeleteProperty(key))
	}
	if len(op) > 0 && op[len(op)-1] == 'I' {
		i, err := key.Uint32()
		if err != nil {
			return napi.Value{}, err
		}
		switch op {
		case "getI":
			return o.GetIndex(i)
		case "setI":
			return napi.Value{}, o.SetIndex(i, val)
		case "hasI":
			return boolResult(o.HasIndex(i))
		case "deleteI":
			return boolResult(o.DeleteIndex(i))
		}
	}
	k, err := key.GoString()
	if err != nil {
		return napi.Value{}, err
	}
	switch op {
	case "get":
		return o.Get(k)
	case "set":
		return napi.Value{}, o.Set(k, val)
	case "has":
		return boolResult(o.Has(k))
	case "hasOwn":
		return boolResult(o.HasOwn(k))
	case "delete":
		return boolResult(o.Delete(k))
	}
	return napi.Value{}, napi.RangeError("", "unknown op "+op)
}

// keys(o, mode): "names" = for...in, "own" = Object.keys, "all" = chiavi proprie con symbol,
// "proto" = prototipo.
func keys(c napi.CallContext) (napi.Value, error) {
	o, err := c.Arg(0).AsObject()
	if err != nil {
		return napi.Value{}, err
	}
	mode, err := c.Arg(1).GoString()
	if err != nil {
		return napi.Value{}, err
	}
	switch mode {
	case "names":
		a, err := o.Keys()
		return a.Value, err
	case "own":
		a, err := o.AllKeys(napi.KeyOwnOnly, napi.KeyEnumerable|napi.KeySkipSymbols, napi.KeyNumbersToStrings)
		return a.Value, err
	case "all":
		a, err := o.AllKeys(napi.KeyOwnOnly, napi.KeyAllProperties, napi.KeyKeepNumbers)
		return a.Value, err
	case "proto":
		return o.Prototype()
	}
	return napi.Value{}, napi.RangeError("", "unknown mode "+mode)
}

// catchJS(fn, ...args) chiama fn: se lancia restituisce {caught: eccezione}, altrimenti
// {value: risultato}. catchJS(fn, ...args) con this === "propagate" lascia propagare l'eccezione.
func catchJS(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	fn, err := c.Arg(0).AsFunction()
	if err != nil {
		return napi.Value{}, err
	}
	// Un this primitivo arriva come oggetto String (le funzioni native non sono strict). Va letto
	// prima della chiamata: con un'eccezione pendente ToString fallirebbe.
	thisStr, err := c.This().ToString()
	if err != nil {
		return napi.Value{}, err
	}
	mode, err := thisStr.GoString()
	if err != nil {
		return napi.Value{}, err
	}
	args := c.Args(nil)
	v, err := fn.Call(env.Undefined(), args[1:]...)
	if mode == "propagate" {
		return v, err
	}
	o, oerr := env.NewObject()
	if oerr != nil {
		return napi.Value{}, oerr
	}
	if exc, ok := env.Catch(err); ok {
		pending := env.IsExceptionPending()
		pv, perr := env.NewBoolean(pending)
		if perr != nil {
			return napi.Value{}, perr
		}
		if err := o.Set("pendingAfterCatch", pv); err != nil {
			return napi.Value{}, err
		}
		return o.Value, o.Set("caught", exc)
	}
	if err != nil {
		return napi.Value{}, err
	}
	return o.Value, o.Set("value", v)
}

// makeFunction(name) crea una funzione che restituisce [numero di argomenti, this, new.target
// presente].
func makeFunction(c napi.CallContext) (napi.Value, error) {
	name, err := c.Arg(0).GoString()
	if err != nil {
		return napi.Value{}, err
	}
	fn, err := c.Env().NewFunction(name, func(c napi.CallContext) (napi.Value, error) {
		env := c.Env()
		a, err := env.NewArray()
		if err != nil {
			return napi.Value{}, err
		}
		n, err := env.NewInt32(int32(c.Len()))
		if err != nil {
			return napi.Value{}, err
		}
		nt, err := env.NewBoolean(c.IsConstructCall())
		if err != nil {
			return napi.Value{}, err
		}
		for i, v := range []napi.Value{n, c.This().Value, nt} {
			if err := a.SetIndex(uint32(i), v); err != nil {
				return napi.Value{}, err
			}
		}
		return a.Value, nil
	})
	return fn.Value, err
}

type codedError struct{ code, msg string }

func (e codedError) Error() string  { return e.msg }
func (e codedError) JSCode() string { return e.code }

// throwError(kind, code, message) restituisce un errore del tipo richiesto.
func throwError(c napi.CallContext) (napi.Value, error) {
	kind, err := c.Arg(0).GoString()
	if err != nil {
		return napi.Value{}, err
	}
	code, _ := c.Arg(1).GoString()
	msg, _ := c.Arg(2).GoString()
	switch kind {
	case "error":
		return napi.Value{}, &napi.Error{Code: code, Message: msg}
	case "type":
		return napi.Value{}, napi.TypeError(code, msg)
	case "range":
		return napi.Value{}, napi.RangeError(code, msg)
	case "syntax":
		return napi.Value{}, napi.SyntaxError(code, msg)
	case "plain":
		return napi.Value{}, errors.New(msg)
	case "coder":
		return napi.Value{}, codedError{code: code, msg: msg}
	case "wrapped":
		// Un *Error avvolto in un altro errore conserva tipo e code.
		return napi.Value{}, errors.Join(napi.RangeError(code, msg))
	case "value":
		return napi.Value{}, napi.Throw(c.Arg(1))
	case "pending":
		// L'eccezione pendente resta quella lanciata qui, non il secondo errore.
		c.Env().Throw(napi.TypeError("FIRST", "first"))
		return napi.Value{}, napi.RangeError("SECOND", "second")
	case "newError":
		e, err := c.Env().NewError(napi.RangeError(code, msg))
		return e.Value, err
	case "napi":
		// Un fallimento di Node-API: GetValueDouble su una stringa.
		s, err := c.Env().NewString("x")
		if err != nil {
			return napi.Value{}, err
		}
		_, err = s.Float64()
		return napi.Value{}, err
	}
	return napi.Value{}, napi.RangeError("", "unknown kind "+kind)
}

// coerce(v, to) applica la coercizione JS richiesta.
func coerce(c napi.CallContext) (napi.Value, error) {
	to, err := c.Arg(1).GoString()
	if err != nil {
		return napi.Value{}, err
	}
	v := c.Arg(0)
	switch to {
	case "string":
		s, err := v.ToString()
		return s.Value, err
	case "number":
		return v.ToNumber()
	case "bool":
		return v.ToBool()
	case "object":
		o, err := v.ToObject()
		return o.Value, err
	}
	return napi.Value{}, napi.RangeError("", "unknown coercion "+to)
}

// compare(a, b, ctor?) restituisce [a === b, a instanceof ctor].
func compare(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	eq, err := c.Arg(0).StrictEquals(c.Arg(1))
	if err != nil {
		return napi.Value{}, err
	}
	inst := false
	if c.Len() > 2 {
		ctor, err := c.Arg(2).AsFunction()
		if err != nil {
			return napi.Value{}, err
		}
		if inst, err = c.Arg(0).InstanceOf(ctor); err != nil {
			return napi.Value{}, err
		}
	}
	a, err := env.NewArray()
	if err != nil {
		return napi.Value{}, err
	}
	for i, b := range []bool{eq, inst} {
		v, err := env.NewBoolean(b)
		if err != nil {
			return napi.Value{}, err
		}
		if err := a.SetIndex(uint32(i), v); err != nil {
			return napi.Value{}, err
		}
	}
	return a.Value, nil
}

// defineProps(o, symbol) definisce su o un metodo, un accessor, un getter senza setter, un
// setter senza getter, un valore con attributi e una proprietà con chiave symbol.
func defineProps(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	o, err := c.Arg(0).AsObject()
	if err != nil {
		return napi.Value{}, err
	}
	stored := 0.0
	answer, err := env.NewInt32(answerValue)
	if err != nil {
		return napi.Value{}, err
	}
	hidden, err := env.NewString("hidden")
	if err != nil {
		return napi.Value{}, err
	}
	get := func(c napi.CallContext) (napi.Value, error) { return c.Env().NewFloat64(stored) }
	set := func(c napi.CallContext) (napi.Value, error) {
		f, err := c.Arg(0).Float64()
		stored = f
		return napi.Value{}, err
	}
	props := []napi.Property{
		{Name: "twice", Method: func(c napi.CallContext) (napi.Value, error) {
			f, err := c.Arg(0).Float64()
			if err != nil {
				return napi.Value{}, err
			}
			return c.Env().NewFloat64(2 * f)
		}, Attributes: napi.DefaultMethod},
		{Name: "acc", Getter: get, Setter: set, Attributes: napi.Enumerable | napi.Configurable},
		{Name: "readOnly", Getter: get, Attributes: napi.Enumerable},
		{Name: "writeOnly", Setter: set},
		{Name: "answer", Value: answer, Attributes: napi.Enumerable},
		{Name: "hidden", Value: hidden.Value, Attributes: napi.Writable},
	}
	if sym, err := c.Arg(1).AsSymbol(); err == nil {
		props = append(props, napi.Property{Key: sym.Value, Value: answer, Attributes: napi.Enumerable})
	}
	return o.Value, o.DefineProperties(props...)
}

// defineCounter definisce la classe Counter(start): metodo inc(step), accessor value, metodo
// statico isCounter(o) basato sul type tag, proprietà statica kind.
func defineCounter(env napi.Env) (napi.Function, error) {
	kind, err := env.NewString("counter")
	if err != nil {
		return napi.Function{}, err
	}
	self := func(c napi.CallContext) (*counter, error) {
		o := c.This()
		ok, err := o.HasTypeTag(counterTag)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, napi.TypeError("ERR_NOT_COUNTER", "not a Counter")
		}
		return napi.UnwrapAs[*counter](o)
	}
	return env.DefineClass("Counter", func(c napi.CallContext) (napi.Value, error) {
		start := 0.0
		if c.Len() > 0 && !c.Arg(0).IsUndefined() {
			f, err := c.Arg(0).Float64()
			if err != nil {
				return napi.Value{}, err
			}
			start = f
		}
		this := c.This()
		if err := this.SetTypeTag(counterTag); err != nil {
			return napi.Value{}, err
		}
		return napi.Value{}, this.Wrap(&counter{n: start}, func(napi.Env, any) { wrapFinalized.Add(1) })
	},
		napi.Property{Name: "inc", Method: func(c napi.CallContext) (napi.Value, error) {
			ct, err := self(c)
			if err != nil {
				return napi.Value{}, err
			}
			step := 1.0
			if c.Len() > 0 {
				if step, err = c.Arg(0).Float64(); err != nil {
					return napi.Value{}, err
				}
			}
			ct.n += step
			return c.Env().NewFloat64(ct.n)
		}, Attributes: napi.DefaultMethod},
		napi.Property{Name: "value",
			Getter: func(c napi.CallContext) (napi.Value, error) {
				ct, err := self(c)
				if err != nil {
					return napi.Value{}, err
				}
				return c.Env().NewFloat64(ct.n)
			},
			Setter: func(c napi.CallContext) (napi.Value, error) {
				ct, err := self(c)
				if err != nil {
					return napi.Value{}, err
				}
				ct.n, err = c.Arg(0).Float64()
				return napi.Value{}, err
			}},
		napi.Property{Name: "isCounter", Method: func(c napi.CallContext) (napi.Value, error) {
			ok := false
			if o, err := c.Arg(0).AsObject(); err == nil {
				if ok, err = o.HasTypeTag(counterTag); err != nil {
					return napi.Value{}, err
				}
			}
			return c.Env().NewBoolean(ok)
		}, Attributes: napi.Static | napi.DefaultMethod},
		napi.Property{Name: "kind", Value: kind.Value, Attributes: napi.Static | napi.Enumerable},
	)
}

// wrap(o, op, n): "wrap" associa n, "unwrap" lo legge, "remove" lo rimuove, "many" crea n
// oggetti con un valore associato (per il test del GC).
func wrap(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	op, err := c.Arg(1).GoString()
	if err != nil {
		return napi.Value{}, err
	}
	if op == "many" {
		n, err := c.Arg(2).Int32()
		if err != nil {
			return napi.Value{}, err
		}
		return napi.Value{}, env.WithScope(func() error {
			for i := int32(0); i < n; i++ {
				o, err := env.NewObject()
				if err != nil {
					return err
				}
				if err := o.Wrap(i, func(napi.Env, any) { wrapFinalized.Add(1) }); err != nil {
					return err
				}
			}
			return nil
		})
	}
	o, err := c.Arg(0).AsObject()
	if err != nil {
		return napi.Value{}, err
	}
	switch op {
	case "wrap":
		n, err := c.Arg(2).Float64()
		if err != nil {
			return napi.Value{}, err
		}
		return napi.Value{}, o.Wrap(n, func(napi.Env, any) { wrapFinalized.Add(1) })
	case "unwrap":
		n, err := napi.UnwrapAs[float64](o)
		if err != nil {
			return napi.Value{}, err
		}
		return env.NewFloat64(n)
	case "unwrapWrongType":
		_, err := napi.UnwrapAs[string](o)
		return napi.Value{}, err
	case "remove":
		v, err := o.RemoveWrap()
		if err != nil {
			return napi.Value{}, err
		}
		return env.NewFloat64(v.(float64))
	}
	return napi.Value{}, napi.RangeError("", "unknown op "+op)
}

// external(op, arg): "new" crea un external con arg, "get" lo legge, "many" ne crea arg (per il
// test del GC).
func external(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	op, err := c.Arg(0).GoString()
	if err != nil {
		return napi.Value{}, err
	}
	switch op {
	case "new":
		s, err := c.Arg(1).GoString()
		if err != nil {
			return napi.Value{}, err
		}
		x, err := env.NewExternal(s, func(napi.Env, any) { externalFinalized.Add(1) })
		return x.Value, err
	case "get":
		x, err := c.Arg(1).AsExternal()
		if err != nil {
			return napi.Value{}, err
		}
		v, err := x.Get()
		if err != nil {
			return napi.Value{}, err
		}
		s, err := env.NewString(v.(string))
		return s.Value, err
	case "many":
		n, err := c.Arg(1).Int32()
		if err != nil {
			return napi.Value{}, err
		}
		return napi.Value{}, env.WithScope(func() error {
			for i := int32(0); i < n; i++ {
				if _, err := env.NewExternal(i, func(napi.Env, any) { externalFinalized.Add(1) }); err != nil {
					return err
				}
			}
			return nil
		})
	}
	return napi.Value{}, napi.RangeError("", "unknown op "+op)
}

func dataOf(env napi.Env) (*envData, error) {
	v, err := env.InstanceData()
	if err != nil {
		return nil, err
	}
	d, ok := v.(*envData)
	if !ok {
		return nil, errors.New("instance data missing")
	}
	return d, nil
}

// refs(op, arg, count): "new" crea una Ref a arg con conteggio count e restituisce il suo id;
// "get", "ref", "unref", "delete" agiscono sulla Ref con id arg.
func refs(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	d, err := dataOf(env)
	if err != nil {
		return napi.Value{}, err
	}
	op, err := c.Arg(0).GoString()
	if err != nil {
		return napi.Value{}, err
	}
	if op == "new" {
		count, err := c.Arg(2).Uint32()
		if err != nil {
			return napi.Value{}, err
		}
		r, err := c.Arg(1).NewRef(count)
		if err != nil {
			return napi.Value{}, err
		}
		d.nextID++
		d.refs[d.nextID] = r
		return env.NewUint32(d.nextID)
	}
	id, err := c.Arg(1).Uint32()
	if err != nil {
		return napi.Value{}, err
	}
	r, ok := d.refs[id]
	if !ok {
		return napi.Value{}, napi.RangeError("", "unknown ref")
	}
	switch op {
	case "get":
		v, err := r.Value()
		if err != nil {
			return napi.Value{}, err
		}
		if v.IsEmpty() {
			s, err := env.NewString("<collected>")
			return s.Value, err
		}
		return v, nil
	case "ref":
		n, err := r.Ref()
		if err != nil {
			return napi.Value{}, err
		}
		return env.NewUint32(n)
	case "unref":
		n, err := r.Unref()
		if err != nil {
			return napi.Value{}, err
		}
		return env.NewUint32(n)
	case "delete":
		delete(d.refs, id)
		return napi.Value{}, r.Delete()
	}
	return napi.Value{}, napi.RangeError("", "unknown op "+op)
}

// buffers(op, arg): crea e legge Buffer e ArrayBuffer. "new", "copy", "external", "externalEmpty",
// "arrayBuffer", "externalArrayBuffer" creano n byte di valore i % byteModulus; "sum" somma i
// byte di un Buffer, "abSum" di un ArrayBuffer; "detach" e "isDetached" agiscono su un ArrayBuffer;
// "alias" scrive 255 nel primo byte attraverso la memoria restituita e restituisce il Buffer.
func buffers(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	op, err := c.Arg(0).GoString()
	if err != nil {
		return napi.Value{}, err
	}
	pattern := func(n int) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(i % byteModulus)
		}
		return b
	}
	sum := func(b []byte) (napi.Value, error) {
		s := 0
		for _, x := range b {
			s += int(x)
		}
		return env.NewInt64(int64(s))
	}
	switch op {
	case "sum":
		b, err := c.Arg(1).AsBuffer()
		if err != nil {
			return napi.Value{}, err
		}
		data, err := b.Bytes()
		if err != nil {
			return napi.Value{}, err
		}
		return sum(data)
	case "abSum":
		ab, err := c.Arg(1).AsArrayBuffer()
		if err != nil {
			return napi.Value{}, err
		}
		data, err := ab.Bytes()
		if err != nil {
			return napi.Value{}, err
		}
		return sum(data)
	case "detach":
		ab, err := c.Arg(1).AsArrayBuffer()
		if err != nil {
			return napi.Value{}, err
		}
		return napi.Value{}, ab.Detach()
	case "isDetached":
		ab, err := c.Arg(1).AsArrayBuffer()
		if err != nil {
			return napi.Value{}, err
		}
		d, err := ab.IsDetached()
		if err != nil {
			return napi.Value{}, err
		}
		return env.NewBoolean(d)
	case "externalEmpty":
		b, err := env.NewExternalBuffer(nil, func(napi.Env) { bufferFinalized.Add(1) })
		return b.Value, err
	}
	n, err := c.Arg(1).Int32()
	if err != nil {
		return napi.Value{}, err
	}
	switch op {
	case "new":
		b, data, err := env.NewBuffer(int(n))
		if err != nil {
			return napi.Value{}, err
		}
		copy(data, pattern(int(n)))
		return b.Value, nil
	case "alias":
		b, data, err := env.NewBuffer(int(n))
		if err != nil {
			return napi.Value{}, err
		}
		if len(data) > 0 {
			data[0] = math.MaxUint8
		}
		return b.Value, nil
	case "copy":
		b, err := env.NewBufferCopy(pattern(int(n)))
		return b.Value, err
	case "external":
		b, err := env.NewExternalBuffer(pattern(int(n)), func(napi.Env) { bufferFinalized.Add(1) })
		return b.Value, err
	case "arrayBuffer":
		ab, data, err := env.NewArrayBuffer(int(n))
		if err != nil {
			return napi.Value{}, err
		}
		copy(data, pattern(int(n)))
		return ab.Value, nil
	case "externalArrayBuffer":
		ab, err := env.NewExternalArrayBuffer(pattern(int(n)), func(napi.Env) { bufferFinalized.Add(1) })
		return ab.Value, err
	}
	return napi.Value{}, napi.RangeError("", "unknown op "+op)
}

// typed(op, ...): "typed"(type, ab, offset, length) crea un TypedArray, "info"(ta) ne descrive
// tipo/lunghezza/offset/byte, "dataView"(ab, offset, length) crea un DataView, "dvInfo"(dv) lo
// descrive. "fillTyped"(ta) scrive l'indice in ogni byte attraverso Info().Data.
func typed(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	op, err := c.Arg(0).GoString()
	if err != nil {
		return napi.Value{}, err
	}
	describe := func(fields map[string]int, ab napi.ArrayBuffer) (napi.Value, error) {
		o, err := env.NewObject()
		if err != nil {
			return napi.Value{}, err
		}
		for k, v := range fields {
			n, err := env.NewInt64(int64(v))
			if err != nil {
				return napi.Value{}, err
			}
			if err := o.Set(k, n); err != nil {
				return napi.Value{}, err
			}
		}
		return o.Value, o.Set("buffer", ab.Value)
	}
	switch op {
	case "typed", "dataView":
		ab, err := c.Arg(2).AsArrayBuffer()
		if err != nil {
			return napi.Value{}, err
		}
		off, err := c.Arg(3).Int32()
		if err != nil {
			return napi.Value{}, err
		}
		length, err := c.Arg(4).Int32()
		if err != nil {
			return napi.Value{}, err
		}
		if op == "dataView" {
			dv, err := env.NewDataView(ab, int(off), int(length))
			return dv.Value, err
		}
		t, err := c.Arg(1).Int32()
		if err != nil {
			return napi.Value{}, err
		}
		ta, err := env.NewTypedArray(napi.TypedArrayType(t), ab, int(off), int(length))
		return ta.Value, err
	case "info", "fillTyped":
		ta, err := c.Arg(1).AsTypedArray()
		if err != nil {
			return napi.Value{}, err
		}
		info, err := ta.Info()
		if err != nil {
			return napi.Value{}, err
		}
		if op == "fillTyped" {
			for i := range info.Data {
				info.Data[i] = byte(i)
			}
			return ta.Value, nil
		}
		return describe(map[string]int{
			"type": int(info.Type), "length": info.Length, "byteOffset": info.ByteOffset,
			"bytes": len(info.Data), "elementSize": napi.ElementSize(info.Type),
		}, info.Buffer)
	case "dvInfo":
		dv, err := c.Arg(1).AsDataView()
		if err != nil {
			return napi.Value{}, err
		}
		info, err := dv.Info()
		if err != nil {
			return napi.Value{}, err
		}
		return describe(map[string]int{"byteOffset": info.ByteOffset, "bytes": len(info.Data)}, info.Buffer)
	}
	return napi.Value{}, napi.RangeError("", "unknown op "+op)
}

// promise(op, value): "resolve" e "reject" concludono subito la Promise con value, "error" la
// rifiuta con un RangeError, "thrown" la rifiuta con value attraverso napi.Throw.
func promise(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	op, err := c.Arg(0).GoString()
	if err != nil {
		return napi.Value{}, err
	}
	p, d, err := env.NewPromise()
	if err != nil {
		return napi.Value{}, err
	}
	switch op {
	case "resolve":
		err = d.Resolve(c.Arg(1))
	case "reject":
		err = d.Reject(c.Arg(1))
	case "error":
		err = d.RejectError(napi.RangeError("E_PROMISE", "rejected from Go"))
	case "thrown":
		err = d.RejectError(napi.Throw(c.Arg(1)))
	default:
		err = napi.RangeError("", "unknown op "+op)
	}
	return p.Value, err
}

// instanceData(v?): con un argomento lo salva nello stato dell'env, poi restituisce il valore
// salvato (0 in un env appena inizializzato).
func instanceData(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	d, err := dataOf(env)
	if err != nil {
		return napi.Value{}, err
	}
	if c.Len() > 0 {
		if d.value, err = c.Arg(0).Float64(); err != nil {
			return napi.Value{}, err
		}
	}
	return env.NewFloat64(d.value)
}

// scopes(n): crea n oggetti in uno scope per iterazione e restituisce un oggetto sopravvissuto a
// uno scope escapable.
func scopes(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	n, err := c.Arg(0).Int32()
	if err != nil {
		return napi.Value{}, err
	}
	for i := int32(0); i < n; i++ {
		if err := env.WithScope(func() error {
			_, err := env.NewObject()
			return err
		}); err != nil {
			return napi.Value{}, err
		}
	}
	// Un errore dentro lo scope arriva al chiamante e lo scope viene chiuso comunque.
	scopeErr := env.WithScope(func() error { return napi.RangeError("E_SCOPE", "inside scope") })
	if scopeErr == nil {
		return napi.Value{}, errors.New("WithScope lost the error")
	}
	return env.WithEscapableScope(func() (napi.Value, error) {
		o, err := env.NewObject()
		if err != nil {
			return napi.Value{}, err
		}
		v, err := env.NewInt32(n)
		if err != nil {
			return napi.Value{}, err
		}
		return o.Value, o.Set("n", v)
	})
}

func versions(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	o, err := env.NewObject()
	if err != nil {
		return napi.Value{}, err
	}
	napiV, err := env.NapiVersion()
	if err != nil {
		return napi.Value{}, err
	}
	nv, err := env.NodeVersion()
	if err != nil {
		return napi.Value{}, err
	}
	file, err := env.ModuleFileName()
	if err != nil && !isUnavailable(err) {
		return napi.Value{}, err
	}
	fields := []struct {
		name string
		make func() (napi.Value, error)
	}{
		{"napi", func() (napi.Value, error) { return env.NewUint32(napiV) }},
		{"node", func() (napi.Value, error) {
			s, err := env.NewString(itoa(nv.Major) + "." + itoa(nv.Minor) + "." + itoa(nv.Patch))
			return s.Value, err
		}},
		{"release", func() (napi.Value, error) { s, err := env.NewString(nv.Release); return s.Value, err }},
		{"file", func() (napi.Value, error) { s, err := env.NewString(file); return s.Value, err }},
	}
	for _, f := range fields {
		v, err := f.make()
		if err != nil {
			return napi.Value{}, err
		}
		if err := o.Set(f.name, v); err != nil {
			return napi.Value{}, err
		}
	}
	return o.Value, nil
}

func isUnavailable(err error) bool {
	var ne *napi.Error
	return errors.As(err, &ne) && ne.Status == napi.StatusUnavailable
}

func itoa(u uint32) string {
	if u == 0 {
		return "0"
	}
	var b [10]byte
	i := len(b)
	for u > 0 {
		i--
		b[i] = byte('0' + u%10)
		u /= 10
	}
	return string(b[i:])
}

// counters restituisce i contatori dei finalizer e di napi.ReadStats.
func counters(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	st := napi.ReadStats()
	o, err := env.NewObject()
	if err != nil {
		return napi.Value{}, err
	}
	for _, f := range []struct {
		name string
		v    int64
	}{
		{"wrapFinalized", wrapFinalized.Load()}, {"externalFinalized", externalFinalized.Load()},
		{"bufferFinalized", bufferFinalized.Load()}, {"cleanupHooksRun", cleanupHooksRun.Load()},
		{"instanceFinalized", instanceFinalized.Load()}, {"inits", inits.Load()},
		{"callbacks", int64(st.Callbacks)}, {"handles", int64(st.Handles)}, {"envs", int64(st.Envs)},
	} {
		v, err := env.NewInt64(f.v)
		if err != nil {
			return napi.Value{}, err
		}
		if err := o.Set(f.name, v); err != nil {
			return napi.Value{}, err
		}
	}
	return o.Value, nil
}

// hotPath(obj, str) esegue le operazioni che non devono allocare: numeri, typeof, lettura di una
// proprietà con chiave stringa e lettura di una stringa corta in un buffer sullo stack (la stringa
// risultante è l'unica allocazione ammessa, ed è assente se vuota).
func hotPath(c napi.CallContext) (napi.Value, error) {
	o, err := c.Arg(0).AsObject()
	if err != nil {
		return napi.Value{}, err
	}
	v, err := o.Get("k")
	if err != nil {
		return napi.Value{}, err
	}
	f, err := v.Float64()
	if err != nil {
		return napi.Value{}, err
	}
	if _, err := c.Arg(1).Type(); err != nil {
		return napi.Value{}, err
	}
	s, err := c.Arg(1).GoString()
	if err != nil {
		return napi.Value{}, err
	}
	return c.Env().NewFloat64(f + float64(len(s)))
}
