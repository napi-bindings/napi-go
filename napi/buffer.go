package napi

import (
	"runtime"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// I []byte restituiti da Bytes, NewBuffer, NewArrayBuffer e Info puntano alla memoria di V8 senza
// copie. Valgono finché l'oggetto JS è vivo (nella callback corrente, o finché una Ref lo tiene)
// e l'ArrayBuffer non viene detached: non vanno conservati né passati a una goroutine senza
// copiarli.

// ---- Buffer ----

// Buffer è un Buffer di Node.js (sottoclasse di Uint8Array).
type Buffer struct{ Object }

// NewBuffer crea un Buffer di size byte (azzerati) e restituisce la sua memoria.
func (e Env) NewBuffer(size int) (Buffer, []byte, error) {
	if size < 0 {
		return Buffer{}, nil, RangeError("", "negative buffer size")
	}
	var data unsafe.Pointer
	var r sys.Value
	st := sys.CreateBuffer(e.raw, uint(size), &data, &r)
	v, err := e.value(r, st)
	if err != nil {
		return Buffer{}, nil, err
	}
	return Buffer{Object{v}}, unsafe.Slice((*byte)(data), size), nil
}

// NewBufferCopy crea un Buffer con una copia di b.
func (e Env) NewBufferCopy(b []byte) (Buffer, error) {
	var r sys.Value
	st := sys.CreateBufferCopy(e.raw, uint(len(b)), unsafe.Pointer(unsafe.SliceData(b)), nil, &r)
	v, err := e.value(r, st)
	return Buffer{Object{v}}, err
}

// NewExternalBuffer crea un Buffer che usa b senza copiarlo: b viene fissato in memoria
// (runtime.Pinner) finché il Buffer vive, poi fin (se non nil) viene chiamata. Dove i buffer esterni
// non sono ammessi (sandbox di V8, alcune build di Electron) b viene copiato e fin chiamata subito.
// Se la creazione fallisce fin non viene chiamata e b resta del chiamante. Il codice Go non deve
// modificare b mentre JS lo usa.
func (e Env) NewExternalBuffer(b []byte, fin func(env Env)) (Buffer, error) {
	if len(b) == 0 {
		if fin != nil {
			fin(e)
		}
		return e.NewBufferCopy(nil)
	}
	id, pinner := pinBytes(b, fin)
	var r sys.Value
	st := sys.CreateExternalBuffer(e.raw, uint(len(b)), uintptr(unsafe.Pointer(&b[0])), sys.FinalizeTrampoline, id, &r)
	if st == sys.StatusNoExternalBuffersAllowed {
		handles.remove(id)
		pinner.Unpin()
		buf, err := e.NewBufferCopy(b)
		if fin != nil {
			fin(e)
		}
		return buf, err
	}
	v, err := e.value(r, st)
	if err != nil {
		handles.remove(id)
		pinner.Unpin()
		return Buffer{}, err
	}
	return Buffer{Object{v}}, nil
}

// pinBytes fissa b e registra l'handle che lo sblocca (e chiama fin) al finalizer.
func pinBytes(b []byte, fin func(env Env)) (uintptr, *runtime.Pinner) {
	pinner := new(runtime.Pinner)
	pinner.Pin(&b[0])
	id := handles.add(&handle{onFinalize: func(e Env, _ any) {
		pinner.Unpin()
		if fin != nil {
			fin(e)
		}
	}})
	return id, pinner
}

// Bytes restituisce la memoria del Buffer (vedi la nota all'inizio del file).
func (b Buffer) Bytes() ([]byte, error) {
	var data unsafe.Pointer
	var n uint
	if err := check(b.env, sys.GetBufferInfo(b.env, b.raw, &data, &n)); err != nil {
		return nil, err
	}
	return unsafe.Slice((*byte)(data), n), nil
}

// ---- ArrayBuffer ----

// ArrayBuffer è un ArrayBuffer JS.
type ArrayBuffer struct{ Object }

// NewArrayBuffer crea un ArrayBuffer di size byte (azzerati) e restituisce la sua memoria.
func (e Env) NewArrayBuffer(size int) (ArrayBuffer, []byte, error) {
	if size < 0 {
		return ArrayBuffer{}, nil, RangeError("", "negative ArrayBuffer size")
	}
	var data unsafe.Pointer
	var r sys.Value
	st := sys.CreateArrayBuffer(e.raw, uint(size), &data, &r)
	v, err := e.value(r, st)
	if err != nil {
		return ArrayBuffer{}, nil, err
	}
	return ArrayBuffer{Object{v}}, unsafe.Slice((*byte)(data), size), nil
}

// NewExternalArrayBuffer crea un ArrayBuffer che usa b senza copiarlo, con le stesse regole di
// NewExternalBuffer (copia dove i buffer esterni non sono ammessi).
func (e Env) NewExternalArrayBuffer(b []byte, fin func(env Env)) (ArrayBuffer, error) {
	if len(b) == 0 {
		if fin != nil {
			fin(e)
		}
		ab, _, err := e.NewArrayBuffer(0)
		return ab, err
	}
	id, pinner := pinBytes(b, fin)
	var r sys.Value
	st := sys.CreateExternalArrayBuffer(e.raw, uintptr(unsafe.Pointer(&b[0])), uint(len(b)), sys.FinalizeTrampoline, id, &r)
	if st == sys.StatusNoExternalBuffersAllowed {
		handles.remove(id)
		pinner.Unpin()
		ab, data, err := e.NewArrayBuffer(len(b))
		if err == nil {
			copy(data, b)
		}
		if fin != nil {
			fin(e)
		}
		return ab, err
	}
	v, err := e.value(r, st)
	if err != nil {
		handles.remove(id)
		pinner.Unpin()
		return ArrayBuffer{}, err
	}
	return ArrayBuffer{Object{v}}, nil
}

// Bytes restituisce la memoria dell'ArrayBuffer (vuota se detached).
func (a ArrayBuffer) Bytes() ([]byte, error) {
	var data unsafe.Pointer
	var n uint
	if err := check(a.env, sys.GetArrayBufferInfo(a.env, a.raw, &data, &n)); err != nil {
		return nil, err
	}
	return unsafe.Slice((*byte)(data), n), nil
}

// Detach stacca la memoria dall'ArrayBuffer (ArrayBuffer.prototype.transfer senza copia): la sua
// lunghezza diventa 0 e i []byte ottenuti prima non vanno più usati.
func (a ArrayBuffer) Detach() error { return check(a.env, sys.DetachArrayBuffer(a.env, a.raw)) }

// IsDetached dice se l'ArrayBuffer è stato staccato.
func (a ArrayBuffer) IsDetached() (bool, error) {
	var b bool
	st := sys.IsDetachedArrayBuffer(a.env, a.raw, &b)
	return b, check(a.env, st)
}

// ---- TypedArray ----

// TypedArrayType è il tipo degli elementi di un TypedArray.
type TypedArrayType = sys.TypedArrayType

const (
	Int8Array         = sys.Int8Array
	Uint8Array        = sys.Uint8Array
	Uint8ClampedArray = sys.Uint8ClampedArray
	Int16Array        = sys.Int16Array
	Uint16Array       = sys.Uint16Array
	Int32Array        = sys.Int32Array
	Uint32Array       = sys.Uint32Array
	Float32Array      = sys.Float32Array
	Float64Array      = sys.Float64Array
	BigInt64Array     = sys.Bigint64Array
	BigUint64Array    = sys.BigUint64Array
	// Float16Array richiede un Node.js che lo supporti in Node-API.
	Float16Array = sys.Float16Array
)

// ElementSize restituisce la dimensione in byte di un elemento (0 per un tipo sconosciuto).
func ElementSize(t TypedArrayType) int {
	switch t {
	case Int8Array, Uint8Array, Uint8ClampedArray:
		return 1
	case Int16Array, Uint16Array, Float16Array:
		return 2
	case Int32Array, Uint32Array, Float32Array:
		return 4
	case Float64Array, BigInt64Array, BigUint64Array:
		return 8
	}
	return 0
}

// TypedArray è un TypedArray JS.
type TypedArray struct{ Object }

// NewTypedArray crea un TypedArray di length elementi su buf a partire da byteOffset (allineato
// alla dimensione degli elementi).
func (e Env) NewTypedArray(t TypedArrayType, buf ArrayBuffer, byteOffset, length int) (TypedArray, error) {
	if byteOffset < 0 || length < 0 {
		return TypedArray{}, RangeError("", "negative TypedArray offset or length")
	}
	var r sys.Value
	st := sys.CreateTypedArray(e.raw, t, uint(length), buf.raw, uint(byteOffset), &r)
	v, err := e.value(r, st)
	return TypedArray{Object{v}}, err
}

// TypedArrayInfo descrive un TypedArray. Data copre gli elementi (Length * ElementSize(Type)
// byte) con le regole di validità della nota all'inizio del file.
type TypedArrayInfo struct {
	Type       TypedArrayType
	Length     int
	ByteOffset int
	Data       []byte
	Buffer     ArrayBuffer
}

// Info restituisce tipo, lunghezza, memoria e ArrayBuffer del TypedArray.
func (t TypedArray) Info() (TypedArrayInfo, error) {
	var typ sys.TypedArrayType
	var n, off uint
	var data unsafe.Pointer
	var ab sys.Value
	if err := check(t.env, sys.GetTypedArrayInfo(t.env, t.raw, &typ, &n, &data, &ab, &off)); err != nil {
		return TypedArrayInfo{}, err
	}
	return TypedArrayInfo{
		Type: typ, Length: int(n), ByteOffset: int(off),
		Data:   unsafe.Slice((*byte)(data), int(n)*ElementSize(typ)),
		Buffer: ArrayBuffer{Object{Value{env: t.env, raw: ab}}},
	}, nil
}

// ---- DataView ----

// DataView è un DataView JS.
type DataView struct{ Object }

// NewDataView crea un DataView di byteLength byte su buf a partire da byteOffset.
func (e Env) NewDataView(buf ArrayBuffer, byteOffset, byteLength int) (DataView, error) {
	if byteOffset < 0 || byteLength < 0 {
		return DataView{}, RangeError("", "negative DataView offset or length")
	}
	var r sys.Value
	st := sys.CreateDataView(e.raw, uint(byteLength), buf.raw, uint(byteOffset), &r)
	v, err := e.value(r, st)
	return DataView{Object{v}}, err
}

// DataViewInfo descrive un DataView.
type DataViewInfo struct {
	ByteOffset int
	Data       []byte
	Buffer     ArrayBuffer
}

// Info restituisce memoria, offset e ArrayBuffer del DataView.
func (d DataView) Info() (DataViewInfo, error) {
	var n, off uint
	var data unsafe.Pointer
	var ab sys.Value
	if err := check(d.env, sys.GetDataViewInfo(d.env, d.raw, &n, &data, &ab, &off)); err != nil {
		return DataViewInfo{}, err
	}
	return DataViewInfo{
		ByteOffset: int(off),
		Data:       unsafe.Slice((*byte)(data), n),
		Buffer:     ArrayBuffer{Object{Value{env: d.env, raw: ab}}},
	}, nil
}
