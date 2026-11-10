package napi

import (
	"reflect"
	"unsafe"
)

// Either holds a value of type A or of type B (napi-rs Either<A, B>), for parameters and results
// that accept two JS types, e.g. Either[string, []byte] for a string or a Buffer. Decoding tries A
// first, then B; encoding converts the value that is set. The zero value holds A's zero value.
type Either[A, B any] struct {
	a   A
	b   B
	isB bool
}

// EitherA returns an Either holding a.
func EitherA[A, B any](a A) Either[A, B] { return Either[A, B]{a: a} }

// EitherB returns an Either holding b.
func EitherB[A, B any](b B) Either[A, B] { return Either[A, B]{b: b, isB: true} }

// A returns the value of type A and whether it is the one set.
func (e Either[A, B]) A() (A, bool) { return e.a, !e.isB }

// B returns the value of type B and whether it is the one set.
func (e Either[A, B]) B() (B, bool) { return e.b, e.isB }

// IsB reports whether the value of type B is the one set.
func (e Either[A, B]) IsB() bool { return e.isB }

// eitherLayout describes an Either instantiation to the codec builder.
type eitherLayout struct {
	ta, tb          reflect.Type
	offA, offB, isB uintptr
}

type eitherMarker interface{ eitherLayout() eitherLayout }

var eitherMarkerType = reflect.TypeFor[eitherMarker]()

func (e Either[A, B]) eitherLayout() eitherLayout {
	return eitherLayout{
		ta: reflect.TypeFor[A](), tb: reflect.TypeFor[B](),
		offA: unsafe.Offsetof(e.a), offB: unsafe.Offsetof(e.b), isB: unsafe.Offsetof(e.isB),
	}
}

func fillEither(c *codec, t reflect.Type) {
	l := reflect.Zero(t).Interface().(eitherMarker).eitherLayout()
	ca, cb := buildCodec(l.ta), buildCodec(l.tb)
	c.jsName = ca.jsName + " or " + cb.jsName
	c.optional = ca.optional || cb.optional
	c.borrows = ca.borrows || cb.borrows
	c.decode = func(v Value, p unsafe.Pointer, m mode) error {
		errA := ca.dec(v, unsafe.Add(p, l.offA), m)
		if errA == nil {
			*(*bool)(unsafe.Add(p, l.isB)) = false
			return nil
		}
		if isPending(errA) {
			return errA
		}
		// A failed: clear what it may have written before trying B.
		reflect.NewAt(l.ta, unsafe.Add(p, l.offA)).Elem().SetZero()
		errB := cb.dec(v, unsafe.Add(p, l.offB), m)
		if errB == nil {
			*(*bool)(unsafe.Add(p, l.isB)) = true
			return nil
		}
		if isPending(errB) {
			return errB
		}
		reflect.NewAt(l.tb, unsafe.Add(p, l.offB)).Elem().SetZero()
		return convTypeError(c.jsName, v)
	}
	c.encode = func(env Env, p unsafe.Pointer) (Value, error) {
		if *(*bool)(unsafe.Add(p, l.isB)) {
			return cb.enc(env, unsafe.Add(p, l.offB))
		}
		return ca.enc(env, unsafe.Add(p, l.offA))
	}
	c.requireElem(ca, "first type")
	c.requireElem(cb, "second type")
}

// isPending reports whether err signals a pending JS exception, which must not be masked by
// trying another conversion.
func isPending(err error) bool {
	ne, ok := err.(*Error)
	if pe, isPath := err.(*pathError); isPath {
		ne, ok = pe.err, true
	}
	return ok && ne.Status == StatusPendingException
}
