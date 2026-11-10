package napi

import (
	"context"
	"errors"
	"math"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"
)

func TestLowerCamel(t *testing.T) {
	cases := map[string]string{
		"Name": "name", "UserID": "userID", "ID": "id", "URLPath": "urlPath", "HTTPServer": "httpServer",
		"X": "x", "A1": "a1", "ABC1": "abc1", "already": "already", "": "", "É": "É",
	}
	for in, want := range cases {
		if got := lowerCamel(in); got != want {
			t.Errorf("lowerCamel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStoreIntBounds(t *testing.T) {
	type tc struct {
		k  primKind
		f  float64
		ok bool
	}
	cases := []tc{
		{primInt8, math.MinInt8, true}, {primInt8, math.MaxInt8, true}, {primInt8, math.MaxInt8 + 1, false},
		{primInt8, math.MinInt8 - 1, false}, {primInt8, 1.5, false}, {primInt8, math.NaN(), false},
		{primInt8, math.Inf(1), false}, {primUint8, -1, false}, {primUint8, math.MaxUint8, true},
		{primInt32, math.MaxInt32, true}, {primInt32, math.MaxInt32 + 1, false},
		{primUint32, math.MaxUint32, true}, {primUint32, math.MaxUint32 + 1, false},
		{primInt64, -math.Pow(2, 63), true}, {primInt64, math.Pow(2, 63), false},
		{primUint64, math.Pow(2, 64), false}, {primUint64, math.Pow(2, 53), true}, {primUint64, -0.0, true},
	}
	for _, c := range cases {
		var buf [8]byte
		err := storeInt(c.f, c.k, unsafe.Pointer(&buf[0]))
		if (err == nil) != c.ok {
			t.Errorf("storeInt(%v, kind %d): err = %v, want ok=%v", c.f, c.k, err, c.ok)
		}
		if err != nil {
			var ne *Error
			if !errors.As(err, &ne) || ne.Kind != KindRangeError || ne.Code != CodeOutOfRange {
				t.Errorf("storeInt(%v): error %v is not a RangeError %s", c.f, err, CodeOutOfRange)
			}
		}
	}
	var i8 int8
	if err := storeInt(-7, primInt8, unsafe.Pointer(&i8)); err != nil || i8 != -7 {
		t.Errorf("storeInt(-7) = %d, %v", i8, err)
	}
	var u64 uint64
	const big53 = 1 << 53
	if err := storeInt(big53, primUint64, unsafe.Pointer(&u64)); err != nil || u64 != big53 {
		t.Errorf("storeInt(2^53) = %d, %v", u64, err)
	}
}

func TestRangeErrorText(t *testing.T) {
	lo, hi := intBounds(primUint64)
	msg := rangeError(-1, lo, hi).(*Error).Message
	if !strings.Contains(msg, "0 to 18446744073709551615") || !strings.HasSuffix(msg, "got -1") {
		t.Errorf("uint64 range message: %q", msg)
	}
	lo, hi = intBounds(primInt64)
	msg = rangeError(math.Inf(1), lo, hi).(*Error).Message
	if !strings.Contains(msg, "-9223372036854775808 to 9223372036854775807") || !strings.HasSuffix(msg, "got Infinity") {
		t.Errorf("int64 range message: %q", msg)
	}
	if got := formatNumber(4294967296); got != "4294967296" {
		t.Errorf("formatNumber(2^32) = %q", got)
	}
	if got := formatNumber(1e22); got != "1e+22" {
		t.Errorf("formatNumber(1e22) = %q", got)
	}
}

type embedded struct {
	ID int
}

type tagged struct {
	embedded
	Name     string
	Age      int    `napi:"years"`
	City     string `json:"city,omitempty"`
	Both     string `napi:"fromNapi" json:"fromJSON"`
	Skip     string `napi:"-"`
	Dash     string `napi:"-,"`
	Opt      *int   `napi:",omitempty"`
	internal int
}

// collectFieldsLocked runs collectFields as codecFor does: under the build lock.
func collectFieldsLocked(t reflect.Type) ([]structField, error) {
	codecBuild.mu.Lock()
	defer codecBuild.mu.Unlock()
	codecBuild.building = map[reflect.Type]*codec{}
	defer func() { codecBuild.building = nil }()
	return collectFields(t, 0, nil)
}

func TestCollectFields(t *testing.T) {
	fields, err := collectFieldsLocked(reflect.TypeFor[tagged]())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	omit := map[string]bool{}
	for _, f := range fields {
		names = append(names, f.name)
		omit[f.name] = f.omitEmpty
	}
	want := []string{"id", "name", "years", "city", "fromNapi", "-", "opt"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("fields = %v, want %v", names, want)
	}
	if !omit["city"] || !omit["opt"] || omit["name"] {
		t.Errorf("omitempty = %v", omit)
	}
	type dup struct {
		A string `napi:"x"`
		B string `napi:"x"`
	}
	if _, err := collectFieldsLocked(reflect.TypeFor[dup]()); err == nil {
		t.Error("duplicate JS names accepted")
	}
}

func TestCodecDirections(t *testing.T) {
	type rec struct {
		Next     *rec
		Children []rec
		Index    map[string]*rec
	}
	type withChan struct{ C chan int }
	cases := []struct {
		t        reflect.Type
		dec, enc bool
		prim     primKind
		optional bool
	}{
		{reflect.TypeFor[float64](), true, true, primFloat64, false},
		{reflect.TypeFor[[]byte](), true, true, primBytes, false},
		{reflect.TypeFor[*int](), true, true, primNone, true},
		{reflect.TypeFor[rec](), true, true, primNone, false},
		{reflect.TypeFor[withChan](), false, false, primNone, false},
		{reflect.TypeFor[map[bool]int](), false, false, primNone, false},
		{reflect.TypeFor[error](), false, true, primNone, true},
		{reflect.TypeFor[any](), true, true, primNone, true},
		{reflect.TypeFor[*big.Int](), true, true, primNone, true},
		{reflect.TypeFor[time.Time](), true, true, primNone, false},
		{reflect.TypeFor[Either[int, string]](), true, true, primNone, false},
		{reflect.TypeFor[Object](), true, true, primNone, false},
		{reflect.TypeFor[Value](), true, true, primNone, true},
		{reflect.TypeFor[func()](), false, false, primNone, false},
	}
	for _, c := range cases {
		cd := codecFor(c.t)
		if (cd.decode != nil) != c.dec || (cd.encode != nil) != c.enc {
			t.Errorf("%s: decode %v encode %v, want %v %v", c.t, cd.decode != nil, cd.encode != nil, c.dec, c.enc)
		}
		if cd.prim != c.prim || cd.optional != c.optional {
			t.Errorf("%s: prim %d optional %v, want %d %v", c.t, cd.prim, cd.optional, c.prim, c.optional)
		}
		if !c.dec && cd.decodeErr == nil || !c.enc && cd.encodeErr == nil {
			t.Errorf("%s: missing direction without a reason", c.t)
		}
	}
	// The cache returns the same codec, and recursive types point to themselves.
	a := codecFor(reflect.TypeFor[rec]())
	b := codecFor(reflect.TypeFor[rec]())
	if a != b {
		t.Error("codec not cached")
	}
}

func TestPrepareChecks(t *testing.T) {
	type check struct {
		def  FuncDef
		want string // substring of the error, "" for success
	}
	cases := []check{
		{Func2(func(a, b float64) float64 { return a + b }), ""},
		{Func2(func(e Env, s string) int { return len(s) }), ""},
		{Func1(func(chan int) int { return 0 }), "parameter 1: type chan int"},
		{Func0(func() func() { return nil }), "result: type func()"},
		{Async1(func(_ context.Context, f Function) (int, error) { return 0, nil }), "refers to JS values"},
		{Async1(func(_ context.Context, b []byte) (int, error) { return len(b), nil }), ""},
		{Async1(func(_ context.Context, c CallContext) (int, error) { return 0, nil }), "cannot be used in a goroutine"},
		{Async0(func(context.Context) (Value, error) { return Value{}, nil }), "cannot be created in a goroutine"},
	}
	for i, c := range cases {
		err := c.def.d.prepare()
		switch {
		case c.want == "" && err != nil:
			t.Errorf("case %d: unexpected error %v", i, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("case %d: error %v, want %q", i, err, c.want)
		}
	}
	// Injected parameters do not count as JS parameters.
	d := Func2(func(e Env, s string) int { return len(s) }).d
	if err := d.prepare(); err != nil || d.jsLen != 1 || d.params[0].js != -1 || d.params[1].js != 0 {
		t.Errorf("injection: jsLen %d params %+v err %v", d.jsLen, d.params, err)
	}
}

func TestFinishPath(t *testing.T) {
	inner := convTypeError("number", Value{})
	err := withPath(withPath(withPath(inner, "[2]"), ".tags"), ".user")
	got := finishPath("argument 1", err).(*Error)
	if got.Message != "argument 1: user.tags[2]: expected number, got nothing" || got.Code != CodeInvalidArgType {
		t.Errorf("finishPath = %q (%s)", got.Message, got.Code)
	}
	if finishPath("argument 1", ErrPendingException) != ErrPendingException {
		t.Error("a pending exception must pass through unchanged")
	}
	plain := errors.New("custom")
	if finishPath("argument 1", plain) != plain || withPath(plain, ".x") != plain {
		t.Error("errors from FromJS implementations must pass through unchanged")
	}
}

func TestEitherAccessors(t *testing.T) {
	a := EitherA[int, string](3)
	if v, ok := a.A(); !ok || v != 3 || a.IsB() {
		t.Errorf("EitherA: %v %v", v, ok)
	}
	b := EitherB[int]("x")
	if v, ok := b.B(); !ok || v != "x" || !b.IsB() {
		t.Errorf("EitherB: %v %v", v, ok)
	}
	if _, ok := b.A(); ok {
		t.Error("EitherB reports A as set")
	}
	l := Either[int8, string]{}.eitherLayout()
	if l.ta != reflect.TypeFor[int8]() || l.tb != reflect.TypeFor[string]() || l.offA == l.offB {
		t.Errorf("layout %+v", l)
	}
}

func TestExportRegistry(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a duplicate export did not panic")
		}
	}()
	Export("dupTest", Const(1))
	Export("dupTest", Const(2))
}
