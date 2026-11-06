package main

// Port of test/array_buffer.cc, test/buffer.cc (with buffer_new_or_copy-inl.h and
// buffer_no_external.cc), test/typedarray.cc and test/dataview/*.cc.
//
// The C++ templates TypedArrayOf<T>, Buffer<T> and the DataView Get/Set accessors read and write
// the elements in place: the Go port does the same through the []byte slices napi returns, viewed
// as the element type with unsafe.Slice (native byte order, as in C++).
//
// bufferNoExternal is the build of buffer_new_or_copy-inl.h with
// NODE_API_NO_EXTERNAL_BUFFERS_ALLOWED: NewOrCopy always copies and calls the finalizer at once.
// napi has no such build option; the port does what NewExternalBuffer does when Node-API reports
// napi_no_external_buffers_allowed.
//
// Not ported: the SharedArrayBuffer variants (experimental Node-API; hasSharedArrayBuffer is
// false, so the scripts skip them).

import (
	"errors"
	"unsafe"

	"github.com/napi-bindings/napi-go/napi"
)

// testLength is the number of elements of the test buffers (testLength in the C++ tests).
const testLength = 4

var (
	errIncorrectLength = errors.New("Incorrect buffer length.")
	errIncorrectData   = errors.New("Incorrect buffer data.")
)

// ---- ArrayBuffer ----

var (
	arrayBufferTestData      [testLength]byte
	arrayBufferFinalizeCount int
)

func initBytes(b []byte) {
	for i := range b {
		b[i] = byte(i)
	}
}

func verifyBytes(b []byte) bool {
	for i := range b {
		if b[i] != byte(i) {
			return false
		}
	}
	return true
}

func samePointer(a, b []byte) bool { return len(a) > 0 && len(b) > 0 && &a[0] == &b[0] }

func initArrayBuffer(env napi.Env) (napi.Object, error) {
	createBuffer := func(c napi.CallContext) (napi.Value, error) {
		ab, data, err := c.Env().NewArrayBuffer(testLength)
		if err != nil {
			return napi.Value{}, err
		}
		if len(data) != testLength {
			return napi.Value{}, errIncorrectLength
		}
		initBytes(data)
		return ab.Value, nil
	}
	// createExternal wraps data in an external ArrayBuffer and checks that it was not copied.
	createExternal := func(c napi.CallContext, data []byte, fin func(napi.Env)) (napi.Value, error) {
		ab, err := c.Env().NewExternalArrayBuffer(data, fin)
		if err != nil {
			return napi.Value{}, err
		}
		got, err := ab.Bytes()
		if err != nil {
			return napi.Value{}, err
		}
		if len(got) != testLength {
			return napi.Value{}, errIncorrectLength
		}
		if !samePointer(got, data) {
			return napi.Value{}, errIncorrectData
		}
		initBytes(data)
		return ab.Value, nil
	}
	withFinalizer := func(c napi.CallContext) (napi.Value, error) {
		arrayBufferFinalizeCount = 0
		return createExternal(c, make([]byte, testLength), func(napi.Env) { arrayBufferFinalizeCount++ })
	}
	argArrayBuffer := func(c napi.CallContext) (napi.ArrayBuffer, error) {
		if !c.Arg(0).IsArrayBuffer() {
			return napi.ArrayBuffer{}, errors.New("A buffer was expected.")
		}
		return c.Arg(0).AsArrayBuffer()
	}
	return newExports(env,
		fn{"createBuffer", createBuffer},
		fn{"createExternalBuffer", func(c napi.CallContext) (napi.Value, error) {
			arrayBufferFinalizeCount = 0
			return createExternal(c, arrayBufferTestData[:], nil)
		}},
		fn{"createExternalBufferWithFinalize", withFinalizer},
		// The finalize hint of the C++ API is data captured by the Go finalizer.
		fn{"createExternalBufferWithFinalizeHint", withFinalizer},
		fn{"checkBuffer", func(c napi.CallContext) (napi.Value, error) {
			ab, err := argArrayBuffer(c)
			if err != nil {
				return napi.Value{}, err
			}
			data, err := ab.Bytes()
			if err != nil {
				return napi.Value{}, err
			}
			if len(data) != testLength {
				return napi.Value{}, errIncorrectLength
			}
			if !verifyBytes(data) {
				return napi.Value{}, errIncorrectData
			}
			return napi.Value{}, nil
		}},
		fn{"getFinalizeCount", func(c napi.CallContext) (napi.Value, error) {
			return c.Env().NewInt32(int32(arrayBufferFinalizeCount))
		}},
		// ArrayBuffer(env, buffer) copies the handle, not the memory: the same object.
		fn{"createBufferWithConstructor", createBuffer},
		fn{"checkEmptyBuffer", func(c napi.CallContext) (napi.Value, error) {
			return c.Env().NewBoolean(napi.ArrayBuffer{}.IsEmpty())
		}},
		fn{"checkDetachUpdatesData", func(c napi.CallContext) (napi.Value, error) {
			ab, err := argArrayBuffer(c)
			if err != nil {
				return napi.Value{}, err
			}
			if _, err := ab.Bytes(); err != nil {
				return napi.Value{}, err
			}
			if detached, err := ab.IsDetached(); err != nil || detached {
				return napi.Value{}, errors.Join(err, errors.New("Buffer should not be detached."))
			}
			if c.Len() == 2 {
				// Detach in JavaScript.
				if !c.Arg(1).IsFunction() {
					return napi.Value{}, errors.New("A function was expected.")
				}
				detach, err := argFunction(c, 1)
				if err != nil {
					return napi.Value{}, err
				}
				if _, err := detach.Call(napi.Value{}); err != nil {
					return napi.Value{}, err
				}
			} else if err := ab.Detach(); err != nil {
				return napi.Value{}, err
			}
			if detached, err := ab.IsDetached(); err != nil || !detached {
				return napi.Value{}, errors.Join(err, errors.New("Buffer should be detached."))
			}
			data, err := ab.Bytes()
			if err != nil {
				return napi.Value{}, err
			}
			if unsafe.SliceData(data) != nil {
				return napi.Value{}, errors.New("Incorrect data pointer.")
			}
			if len(data) != 0 {
				return napi.Value{}, errIncorrectLength
			}
			return napi.Value{}, nil
		}},
	)
}

// ---- Buffer<uint16_t> ----

const uint16Bytes = 2

var (
	bufferTestData      [testLength]uint16
	bufferFinalizeCount int
)

// asUint16 views a byte slice as uint16 elements, like Buffer<uint16_t>::Data().
func asUint16(b []byte) []uint16 {
	if len(b) < uint16Bytes {
		return nil
	}
	return unsafe.Slice((*uint16)(unsafe.Pointer(&b[0])), len(b)/uint16Bytes)
}

// bytesOfUint16 views uint16 elements as bytes, to hand them to Node-API.
func bytesOfUint16(u []uint16) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(&u[0])), len(u)*uint16Bytes)
}

func initUint16(u []uint16) {
	for i := range u {
		u[i] = uint16(i)
	}
}

func verifyUint16(u []uint16) bool {
	for i := range u {
		if u[i] != uint16(i) {
			return false
		}
	}
	return true
}

func countBufferFinalize(napi.Env) { bufferFinalizeCount++ }

func initBuffer(env napi.Env) (napi.Object, error) {
	// external creates an external Buffer over data and checks that it was not copied.
	external := func(c napi.CallContext, data []uint16, fin func(napi.Env)) (napi.Value, error) {
		buf, err := c.Env().NewExternalBuffer(bytesOfUint16(data), fin)
		if err != nil {
			return napi.Value{}, err
		}
		got, err := buf.Bytes()
		if err != nil {
			return napi.Value{}, err
		}
		if len(asUint16(got)) != testLength {
			return napi.Value{}, errIncorrectLength
		}
		if &asUint16(got)[0] != &data[0] {
			return napi.Value{}, errIncorrectData
		}
		initUint16(data)
		return buf.Value, nil
	}
	externalWithFinalizer := func(c napi.CallContext) (napi.Value, error) {
		bufferFinalizeCount = 0
		return external(c, make([]uint16, testLength), countBufferFinalize)
	}
	// newOrCopy is Buffer<uint16_t>::NewOrCopy: external when allowed, otherwise a copy.
	newOrCopy := func(c napi.CallContext, data []uint16, fin func(napi.Env)) (napi.Value, error) {
		initUint16(data)
		buf, err := c.Env().NewExternalBuffer(bytesOfUint16(data), fin)
		if err != nil {
			return napi.Value{}, err
		}
		got, err := buf.Bytes()
		if err != nil {
			return napi.Value{}, err
		}
		if len(asUint16(got)) != testLength {
			return napi.Value{}, errIncorrectLength
		}
		return buf.Value, nil
	}
	return newExports(env,
		fn{"createBuffer", func(c napi.CallContext) (napi.Value, error) {
			buf, data, err := c.Env().NewBuffer(testLength * uint16Bytes)
			if err != nil {
				return napi.Value{}, err
			}
			u := asUint16(data)
			if len(u) != testLength {
				return napi.Value{}, errIncorrectLength
			}
			initUint16(u)
			return buf.Value, nil
		}},
		fn{"createExternalBuffer", func(c napi.CallContext) (napi.Value, error) {
			bufferFinalizeCount = 0
			return external(c, bufferTestData[:], nil)
		}},
		fn{"createExternalBufferWithFinalize", externalWithFinalizer},
		fn{"createExternalBufferWithFinalizeHint", externalWithFinalizer},
		fn{"createOrCopyExternalBuffer", func(c napi.CallContext) (napi.Value, error) {
			bufferFinalizeCount = 0
			return newOrCopy(c, bufferTestData[:], nil)
		}},
		fn{"createOrCopyExternalBufferWithFinalize", func(c napi.CallContext) (napi.Value, error) {
			bufferFinalizeCount = 0
			return newOrCopy(c, make([]uint16, testLength), countBufferFinalize)
		}},
		fn{"createOrCopyExternalBufferWithFinalizeHint", func(c napi.CallContext) (napi.Value, error) {
			bufferFinalizeCount = 0
			return newOrCopy(c, make([]uint16, testLength), countBufferFinalize)
		}},
		fn{"createBufferCopy", func(c napi.CallContext) (napi.Value, error) {
			initUint16(bufferTestData[:])
			buf, err := c.Env().NewBufferCopy(bytesOfUint16(bufferTestData[:]))
			if err != nil {
				return napi.Value{}, err
			}
			got, err := buf.Bytes()
			if err != nil {
				return napi.Value{}, err
			}
			u := asUint16(got)
			if len(u) != testLength {
				return napi.Value{}, errIncorrectLength
			}
			if &u[0] == &bufferTestData[0] {
				return napi.Value{}, errors.New("Copy should have different memory.")
			}
			if !verifyUint16(u) {
				return napi.Value{}, errors.New("Copy data is incorrect.")
			}
			return buf.Value, nil
		}},
		fn{"checkBuffer", func(c napi.CallContext) (napi.Value, error) {
			if !c.Arg(0).IsBuffer() {
				return napi.Value{}, errors.New("A buffer was expected.")
			}
			buf, err := c.Arg(0).AsBuffer()
			if err != nil {
				return napi.Value{}, err
			}
			data, err := buf.Bytes()
			if err != nil {
				return napi.Value{}, err
			}
			u := asUint16(data)
			if len(u) != testLength {
				return napi.Value{}, errIncorrectLength
			}
			if !verifyUint16(u) {
				return napi.Value{}, errIncorrectData
			}
			return napi.Value{}, nil
		}},
		fn{"getFinalizeCount", func(c napi.CallContext) (napi.Value, error) {
			return c.Env().NewInt32(int32(bufferFinalizeCount))
		}},
	)
}

// initBufferNoExternal is buffer_no_external.cc: NewOrCopy in a build where external buffers are
// not allowed, so the data is copied and the finalizer runs before the Buffer is returned.
func initBufferNoExternal(env napi.Env) (napi.Object, error) {
	copyAndFinalize := func(c napi.CallContext, data []uint16, fin func(napi.Env)) (napi.Value, error) {
		initUint16(data)
		buf, err := c.Env().NewBufferCopy(bytesOfUint16(data))
		if err != nil {
			return napi.Value{}, err
		}
		if fin != nil {
			fin(c.Env())
		}
		return buf.Value, nil
	}
	withFinalizer := func(c napi.CallContext) (napi.Value, error) {
		bufferFinalizeCount = 0
		return copyAndFinalize(c, make([]uint16, testLength), countBufferFinalize)
	}
	return newExports(env,
		fn{"createOrCopyExternalBuffer", func(c napi.CallContext) (napi.Value, error) {
			bufferFinalizeCount = 0
			return copyAndFinalize(c, bufferTestData[:], nil)
		}},
		fn{"createOrCopyExternalBufferWithFinalize", withFinalizer},
		fn{"createOrCopyExternalBufferWithFinalizeHint", withFinalizer},
	)
}

// ---- TypedArray ----

// typedArrayNames maps the type names used by test/typedarray.js to Node-API types.
var typedArrayNames = []struct {
	name string
	typ  napi.TypedArrayType
}{
	{"int8", napi.Int8Array}, {"uint8", napi.Uint8Array}, {"uint8_clamped", napi.Uint8ClampedArray},
	{"int16", napi.Int16Array}, {"uint16", napi.Uint16Array}, {"int32", napi.Int32Array},
	{"uint32", napi.Uint32Array}, {"float32", napi.Float32Array}, {"float64", napi.Float64Array},
	{"bigint64", napi.BigInt64Array}, {"biguint64", napi.BigUint64Array},
}

var errInvalidTypedArrayType = errors.New("Invalid typed-array type.")

// elements views the bytes of a TypedArray as its element type T.
func elements[T any](data []byte) []T {
	var zero T
	size := int(unsafe.Sizeof(zero))
	if len(data) < size {
		return nil
	}
	return unsafe.Slice((*T)(unsafe.Pointer(&data[0])), len(data)/size)
}

func equalElements[T comparable](a, b []byte) bool {
	ea, eb := elements[T](a), elements[T](b)
	if len(ea) != len(eb) {
		return false
	}
	for i := range ea {
		if ea[i] != eb[i] {
			return false
		}
	}
	return true
}

func argTypedArray(c napi.CallContext, i int) (napi.TypedArrayInfo, error) {
	t, err := c.Arg(i).AsTypedArray()
	if err != nil {
		return napi.TypedArrayInfo{}, err
	}
	return t.Info()
}

func initTypedArray(env napi.Env) (napi.Object, error) {
	info := func(get func(c napi.CallContext, ti napi.TypedArrayInfo) (napi.Value, error)) napi.Callback {
		return func(c napi.CallContext) (napi.Value, error) {
			ti, err := argTypedArray(c, 0)
			if err != nil {
				return napi.Value{}, err
			}
			return get(c, ti)
		}
	}
	bufferOf := info(func(c napi.CallContext, ti napi.TypedArrayInfo) (napi.Value, error) { return ti.Buffer.Value, nil })
	return newExports(env,
		fn{"createTypedArray", createTypedArray},
		fn{"createInvalidTypedArray", func(c napi.CallContext) (napi.Value, error) {
			t, err := c.Env().NewTypedArray(napi.Int8Array, napi.ArrayBuffer{}, 0, 1)
			return t.Value, err
		}},
		fn{"getTypedArrayType", info(func(c napi.CallContext, ti napi.TypedArrayInfo) (napi.Value, error) {
			for _, t := range typedArrayNames {
				if t.typ == ti.Type {
					return str(c.Env(), t.name)
				}
			}
			return str(c.Env(), "invalid")
		})},
		fn{"getTypedArrayLength", info(func(c napi.CallContext, ti napi.TypedArrayInfo) (napi.Value, error) {
			return c.Env().NewFloat64(float64(ti.Length))
		})},
		fn{"getTypedArraySize", info(func(c napi.CallContext, ti napi.TypedArrayInfo) (napi.Value, error) {
			return c.Env().NewFloat64(float64(napi.ElementSize(ti.Type)))
		})},
		fn{"getTypedArrayByteOffset", info(func(c napi.CallContext, ti napi.TypedArrayInfo) (napi.Value, error) {
			return c.Env().NewFloat64(float64(ti.ByteOffset))
		})},
		fn{"getTypedArrayByteLength", info(func(c napi.CallContext, ti napi.TypedArrayInfo) (napi.Value, error) {
			return c.Env().NewFloat64(float64(len(ti.Data)))
		})},
		fn{"getTypedArrayBuffer", bufferOf},
		fn{"getTypedArrayBufferValue", bufferOf},
		fn{"getTypedArrayElement", func(c napi.CallContext) (napi.Value, error) {
			ti, err := argTypedArray(c, 0)
			if err != nil {
				return napi.Value{}, err
			}
			i, err := argUint32(c, 1)
			if err != nil {
				return napi.Value{}, err
			}
			return getElement(c.Env(), ti, int(i))
		}},
		fn{"setTypedArrayElement", func(c napi.CallContext) (napi.Value, error) {
			ti, err := argTypedArray(c, 0)
			if err != nil {
				return napi.Value{}, err
			}
			i, err := argUint32(c, 1)
			if err != nil {
				return napi.Value{}, err
			}
			return napi.Value{}, setElement(ti, int(i), c.Arg(2))
		}},
		fn{"checkBufferContent", func(c napi.CallContext) (napi.Value, error) {
			a, err := argTypedArray(c, 0)
			if err != nil {
				return napi.Value{}, err
			}
			b, err := argTypedArray(c, 1)
			if err != nil {
				return napi.Value{}, err
			}
			var eq bool
			switch a.Type {
			case napi.Int8Array, napi.Uint8Array, napi.Uint8ClampedArray:
				eq = equalElements[uint8](a.Data, b.Data)
			case napi.Int16Array, napi.Uint16Array:
				eq = equalElements[uint16](a.Data, b.Data)
			case napi.Int32Array, napi.Uint32Array:
				eq = equalElements[uint32](a.Data, b.Data)
			case napi.Float32Array:
				eq = equalElements[float32](a.Data, b.Data)
			case napi.Float64Array:
				eq = equalElements[float64](a.Data, b.Data)
			case napi.BigInt64Array, napi.BigUint64Array:
				eq = equalElements[uint64](a.Data, b.Data)
			}
			return c.Env().NewBoolean(eq)
		}},
	)
}

// createTypedArray(type, length[, buffer[, offset]]): without a buffer, TypedArrayOf<T>::New
// allocates a new ArrayBuffer of length elements.
func createTypedArray(c napi.CallContext) (napi.Value, error) {
	name, err := argString(c, 0)
	if err != nil {
		return napi.Value{}, err
	}
	length, err := argUint32(c, 1)
	if err != nil {
		return napi.Value{}, err
	}
	var offset uint32
	if !c.Arg(3).IsUndefined() {
		if offset, err = argUint32(c, 3); err != nil {
			return napi.Value{}, err
		}
	}
	for _, t := range typedArrayNames {
		if t.name != name {
			continue
		}
		var ab napi.ArrayBuffer
		if c.Arg(2).IsUndefined() {
			if ab, _, err = c.Env().NewArrayBuffer(int(length) * napi.ElementSize(t.typ)); err != nil {
				return napi.Value{}, err
			}
		} else if ab, err = c.Arg(2).AsArrayBuffer(); err != nil {
			return napi.Value{}, err
		}
		ta, err := c.Env().NewTypedArray(t.typ, ab, int(offset), int(length))
		return ta.Value, err
	}
	return napi.Value{}, errInvalidTypedArrayType
}

func getElement(env napi.Env, ti napi.TypedArrayInfo, i int) (napi.Value, error) {
	switch ti.Type {
	case napi.Int8Array:
		return env.NewInt32(int32(elements[int8](ti.Data)[i]))
	case napi.Uint8Array, napi.Uint8ClampedArray:
		return env.NewUint32(uint32(elements[uint8](ti.Data)[i]))
	case napi.Int16Array:
		return env.NewInt32(int32(elements[int16](ti.Data)[i]))
	case napi.Uint16Array:
		return env.NewUint32(uint32(elements[uint16](ti.Data)[i]))
	case napi.Int32Array:
		return env.NewInt32(elements[int32](ti.Data)[i])
	case napi.Uint32Array:
		return env.NewUint32(elements[uint32](ti.Data)[i])
	case napi.Float32Array:
		return env.NewFloat64(float64(elements[float32](ti.Data)[i]))
	case napi.Float64Array:
		return env.NewFloat64(elements[float64](ti.Data)[i])
	case napi.BigInt64Array:
		b, err := env.NewBigIntInt64(elements[int64](ti.Data)[i])
		return b.Value, err
	case napi.BigUint64Array:
		b, err := env.NewBigIntUint64(elements[uint64](ti.Data)[i])
		return b.Value, err
	}
	return napi.Value{}, errInvalidTypedArrayType
}

func setElement(ti napi.TypedArrayInfo, i int, v napi.Value) error {
	switch ti.Type {
	case napi.Int8Array, napi.Int16Array, napi.Int32Array:
		n, err := v.Int32()
		if err != nil {
			return err
		}
		switch ti.Type {
		case napi.Int8Array:
			elements[int8](ti.Data)[i] = int8(n)
		case napi.Int16Array:
			elements[int16](ti.Data)[i] = int16(n)
		default:
			elements[int32](ti.Data)[i] = n
		}
	case napi.Uint8Array, napi.Uint8ClampedArray, napi.Uint16Array, napi.Uint32Array:
		n, err := v.Uint32()
		if err != nil {
			return err
		}
		switch ti.Type {
		case napi.Uint16Array:
			elements[uint16](ti.Data)[i] = uint16(n)
		case napi.Uint32Array:
			elements[uint32](ti.Data)[i] = n
		default:
			elements[uint8](ti.Data)[i] = uint8(n)
		}
	case napi.Float32Array, napi.Float64Array:
		f, err := v.Float64()
		if err != nil {
			return err
		}
		if ti.Type == napi.Float32Array {
			elements[float32](ti.Data)[i] = float32(f)
		} else {
			elements[float64](ti.Data)[i] = f
		}
	case napi.BigInt64Array, napi.BigUint64Array:
		b, err := v.AsBigInt()
		if err != nil {
			return err
		}
		if ti.Type == napi.BigInt64Array {
			n, _, err := b.Int64()
			elements[int64](ti.Data)[i] = n
			return err
		}
		n, _, err := b.Uint64()
		elements[uint64](ti.Data)[i] = n
		return err
	default:
		return errInvalidTypedArrayType
	}
	return nil
}

// ---- DataView ----

func initDataView(env napi.Env) (napi.Object, error) {
	// create builds DataView::New(env, arrayBuffer[, byteOffset[, byteLength]]) from argc
	// arguments; byteLength defaults to the rest of the buffer.
	create := func(argc int) napi.Callback {
		return func(c napi.CallContext) (napi.Value, error) {
			ab, err := c.Arg(0).AsArrayBuffer()
			if err != nil {
				return napi.Value{}, err
			}
			data, err := ab.Bytes()
			if err != nil {
				return napi.Value{}, err
			}
			var offset, length uint32
			if argc > 1 {
				if offset, err = argUint32(c, 1); err != nil {
					return napi.Value{}, err
				}
			}
			if argc > 2 {
				if length, err = argUint32(c, 2); err != nil {
					return napi.Value{}, err
				}
			} else {
				length = uint32(len(data)) - offset
				if int(offset) > len(data) {
					return napi.Value{}, napi.RangeError("", "Start offset is outside the bounds of the buffer")
				}
			}
			dv, err := c.Env().NewDataView(ab, int(offset), int(length))
			return dv.Value, err
		}
	}
	infoOf := func(get func(c napi.CallContext, di napi.DataViewInfo) (napi.Value, error)) napi.Callback {
		return func(c napi.CallContext) (napi.Value, error) {
			dv, err := c.Arg(0).AsDataView()
			if err != nil {
				return napi.Value{}, err
			}
			di, err := dv.Info()
			if err != nil {
				return napi.Value{}, err
			}
			return get(c, di)
		}
	}
	bufferOf := infoOf(func(c napi.CallContext, di napi.DataViewInfo) (napi.Value, error) { return di.Buffer.Value, nil })
	return newExports(env,
		fn{"createDataView", create(1)},
		fn{"createDataViewWithByteOffset", create(2)},
		fn{"createDataViewWithByteOffsetAndByteLength", create(3)},
		fn{"getArrayBuffer", bufferOf},
		fn{"getBuffer", bufferOf},
		fn{"getByteOffset", infoOf(func(c napi.CallContext, di napi.DataViewInfo) (napi.Value, error) {
			return c.Env().NewFloat64(float64(di.ByteOffset))
		})},
		fn{"getByteLength", infoOf(func(c napi.CallContext, di napi.DataViewInfo) (napi.Value, error) {
			return c.Env().NewFloat64(float64(len(di.Data)))
		})},
	)
}

// dataViewSlot returns the size bytes of the DataView at the offset passed as second argument,
// or the RangeError node-addon-api throws when they are out of bounds.
func dataViewSlot(c napi.CallContext, size int) ([]byte, error) {
	dv, err := c.Arg(0).AsDataView()
	if err != nil {
		return nil, err
	}
	di, err := dv.Info()
	if err != nil {
		return nil, err
	}
	offset, err := argUint32(c, 1)
	if err != nil {
		return nil, err
	}
	if int(offset)+size > len(di.Data) {
		return nil, napi.RangeError("", "Offset is outside the bounds of the DataView")
	}
	return di.Data[offset : int(offset)+size], nil
}

// dataViewGet and dataViewSet read and write a T at the given offset in native byte order, like
// DataView::GetXxx / SetXxx.
func dataViewGet[T int8 | uint8 | int16 | uint16 | int32 | uint32 | float32 | float64]() napi.Callback {
	return func(c napi.CallContext) (napi.Value, error) {
		var zero T
		b, err := dataViewSlot(c, int(unsafe.Sizeof(zero)))
		if err != nil {
			return napi.Value{}, err
		}
		// Copy rather than dereference: the offset need not be aligned for T.
		var v T
		copy(unsafe.Slice((*byte)(unsafe.Pointer(&v)), len(b)), b)
		return c.Env().NewFloat64(float64(v))
	}
}

func dataViewSet[T int8 | uint8 | int16 | uint16 | int32 | uint32 | float32 | float64](read func(napi.Value) (T, error)) napi.Callback {
	return func(c napi.CallContext) (napi.Value, error) {
		var zero T
		b, err := dataViewSlot(c, int(unsafe.Sizeof(zero)))
		if err != nil {
			return napi.Value{}, err
		}
		v, err := read(c.Arg(2))
		if err != nil {
			return napi.Value{}, err
		}
		copy(b, unsafe.Slice((*byte)(unsafe.Pointer(&v)), len(b)))
		return napi.Value{}, nil
	}
}

func initDataViewReadWrite(env napi.Env) (napi.Object, error) {
	i32 := func(v napi.Value) (int32, error) { return v.Int32() }
	u32 := func(v napi.Value) (uint32, error) { return v.Uint32() }
	return newExports(env,
		fn{"getFloat32", dataViewGet[float32]()},
		fn{"getFloat64", dataViewGet[float64]()},
		fn{"getInt8", dataViewGet[int8]()},
		fn{"getInt16", dataViewGet[int16]()},
		fn{"getInt32", dataViewGet[int32]()},
		fn{"getUint8", dataViewGet[uint8]()},
		fn{"getUint16", dataViewGet[uint16]()},
		fn{"getUint32", dataViewGet[uint32]()},
		fn{"setFloat32", dataViewSet(func(v napi.Value) (float32, error) { f, err := v.Float64(); return float32(f), err })},
		fn{"setFloat64", dataViewSet(napi.Value.Float64)},
		fn{"setInt8", dataViewSet(func(v napi.Value) (int8, error) { i, err := i32(v); return int8(i), err })},
		fn{"setInt16", dataViewSet(func(v napi.Value) (int16, error) { i, err := i32(v); return int16(i), err })},
		fn{"setInt32", dataViewSet(i32)},
		fn{"setUint8", dataViewSet(func(v napi.Value) (uint8, error) { u, err := u32(v); return uint8(u), err })},
		fn{"setUint16", dataViewSet(func(v napi.Value) (uint16, error) { u, err := u32(v); return uint16(u), err })},
		fn{"setUint32", dataViewSet(u32)},
	)
}
