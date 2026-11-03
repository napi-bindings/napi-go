package napi

import (
	"runtime"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// The []byte slices returned by Bytes, NewBuffer, NewArrayBuffer, and Info point directly to V8
// memory without copying. They remain valid as long as the JS object is alive (during the current
// callback, or while held by a Ref) and the ArrayBuffer is not detached: do not retain them or
// pass them to a goroutine without copying first.

// ---- Buffer ----

// Buffer represents a Node.js Buffer (a subclass of Uint8Array).
type Buffer struct{ Object }

// NewBuffer creates a zero-initialized Buffer of size bytes and returns its underlying memory.
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

// NewBufferCopy creates a Buffer initialized with a copy of b.
func (e Env) NewBufferCopy(b []byte) (Buffer, error) {
	var r sys.Value
	st := sys.CreateBufferCopy(e.raw, uint(len(b)), unsafe.Pointer(unsafe.SliceData(b)), nil, &r)
	v, err := e.value(r, st)
	return Buffer{Object{v}}, err
}

// NewExternalBuffer creates a Buffer backed by b without copying: b is pinned in memory
// (runtime.Pinner) for the lifetime of the Buffer, after which fin (if non-nil) is called. In
// environments where external buffers are not allowed (V8 sandbox, some Electron builds), b is
// copied and fin is called immediately. If creation fails, fin is not called and b remains owned
// by the caller. Go code must not modify b while JS is using it.
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

// pinBytes pins b and registers a handle that unpins it (and calls fin) when finalized.
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

// Bytes returns the underlying memory of the Buffer (see the note at the top of the file).
func (b Buffer) Bytes() ([]byte, error) {
	var data unsafe.Pointer
	var n uint
	if err := check(b.env, sys.GetBufferInfo(b.env, b.raw, &data, &n)); err != nil {
		return nil, err
	}
	return unsafe.Slice((*byte)(data), n), nil
}

// ---- ArrayBuffer ----

// ArrayBuffer represents a JS ArrayBuffer.
type ArrayBuffer struct{ Object }

// NewArrayBuffer creates a zero-initialized ArrayBuffer of size bytes and returns its underlying memory.
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

// NewExternalArrayBuffer creates an ArrayBuffer backed by b without copying, following the same
// rules as NewExternalBuffer (falling back to a copy where external buffers are not allowed).
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

// Bytes returns the underlying memory of the ArrayBuffer (empty if detached).
func (a ArrayBuffer) Bytes() ([]byte, error) {
	var data unsafe.Pointer
	var n uint
	if err := check(a.env, sys.GetArrayBufferInfo(a.env, a.raw, &data, &n)); err != nil {
		return nil, err
	}
	return unsafe.Slice((*byte)(data), n), nil
}

// Detach detaches the underlying memory from the ArrayBuffer (like ArrayBuffer.prototype.transfer
// without copying): its byte length becomes 0 and any previously obtained []byte slice must no
// longer be used.
func (a ArrayBuffer) Detach() error { return check(a.env, sys.DetachArrayBuffer(a.env, a.raw)) }

// IsDetached reports whether the ArrayBuffer has been detached.
func (a ArrayBuffer) IsDetached() (bool, error) {
	var b bool
	st := sys.IsDetachedArrayBuffer(a.env, a.raw, &b)
	return b, check(a.env, st)
}

// ---- TypedArray ----

// TypedArrayType represents the element type of a TypedArray.
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
	// Float16Array requires a Node.js version with Float16Array support in Node-API.
	Float16Array = sys.Float16Array
)

// ElementSize returns the size in bytes of a single element (0 for an unknown type).
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

// TypedArray represents a JS TypedArray.
type TypedArray struct{ Object }

// NewTypedArray creates a TypedArray of length elements over buf starting at byteOffset (which
// must be aligned to the element size).
func (e Env) NewTypedArray(t TypedArrayType, buf ArrayBuffer, byteOffset, length int) (TypedArray, error) {
	if byteOffset < 0 || length < 0 {
		return TypedArray{}, RangeError("", "negative TypedArray offset or length")
	}
	var r sys.Value
	st := sys.CreateTypedArray(e.raw, t, uint(length), buf.raw, uint(byteOffset), &r)
	v, err := e.value(r, st)
	return TypedArray{Object{v}}, err
}

// TypedArrayInfo describes a TypedArray. Data spans the elements (Length * ElementSize(Type)
// bytes) and is subject to the lifetime rules described at the top of the file.
type TypedArrayInfo struct {
	Type       TypedArrayType
	Length     int
	ByteOffset int
	Data       []byte
	Buffer     ArrayBuffer
}

// Info returns the element type, length, underlying memory, and ArrayBuffer of the TypedArray.
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

// DataView represents a JS DataView.
type DataView struct{ Object }

// NewDataView creates a DataView of byteLength bytes over buf starting at byteOffset.
func (e Env) NewDataView(buf ArrayBuffer, byteOffset, byteLength int) (DataView, error) {
	if byteOffset < 0 || byteLength < 0 {
		return DataView{}, RangeError("", "negative DataView offset or length")
	}
	var r sys.Value
	st := sys.CreateDataView(e.raw, uint(byteLength), buf.raw, uint(byteOffset), &r)
	v, err := e.value(r, st)
	return DataView{Object{v}}, err
}

// DataViewInfo describes a DataView.
type DataViewInfo struct {
	ByteOffset int
	Data       []byte
	Buffer     ArrayBuffer
}

// Info returns the underlying memory, byte offset, and ArrayBuffer of the DataView.
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
