package napi

import (
	"math"
	"math/big"
	"time"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// ---- number e boolean ----

// NewFloat64 crea un number.
func (e Env) NewFloat64(f float64) (Value, error) {
	var r sys.Value
	st := sys.CreateDouble(e.raw, f, &r)
	return e.value(r, st)
}

// NewInt32 crea un number da un int32.
func (e Env) NewInt32(i int32) (Value, error) {
	var r sys.Value
	st := sys.CreateInt32(e.raw, i, &r)
	return e.value(r, st)
}

// NewUint32 crea un number da un uint32.
func (e Env) NewUint32(u uint32) (Value, error) {
	var r sys.Value
	st := sys.CreateUint32(e.raw, u, &r)
	return e.value(r, st)
}

// NewInt64 crea un number da un int64. Oltre ±2^53 il valore perde precisione (come in JS); per
// gli interi grandi si usa NewBigIntInt64.
func (e Env) NewInt64(i int64) (Value, error) {
	var r sys.Value
	st := sys.CreateInt64(e.raw, i, &r)
	return e.value(r, st)
}

// NewBoolean restituisce true o false.
func (e Env) NewBoolean(b bool) (Value, error) {
	var r sys.Value
	st := sys.GetBoolean(e.raw, b, &r)
	return e.value(r, st)
}

// ---- string ----

// String è un valore string.
type String struct{ Value }

// NewString crea una stringa da UTF-8. I byte di s vengono passati a V8 senza copie lato Go.
func (e Env) NewString(s string) (String, error) {
	var r sys.Value
	st := sys.CreateStringUTF8(e.raw, unsafe.StringData(s), uint(len(s)), &r)
	v, err := e.value(r, st)
	return String{v}, err
}

// NewStringUTF16 crea una stringa da unità UTF-16.
func (e Env) NewStringUTF16(s []uint16) (String, error) {
	var r sys.Value
	st := sys.CreateStringUTF16(e.raw, unsafe.SliceData(s), uint(len(s)), &r)
	v, err := e.value(r, st)
	return String{v}, err
}

// NewStringLatin1 crea una stringa da byte ISO-8859-1 (un byte per carattere).
func (e Env) NewStringLatin1(s []byte) (String, error) {
	var r sys.Value
	st := sys.CreateStringLatin1(e.raw, unsafe.SliceData(s), uint(len(s)), &r)
	v, err := e.value(r, st)
	return String{v}, err
}

// propertyKey crea la chiave di una proprietà: una stringa interna di V8
// (node_api_create_property_key_utf8, Node-API 10) se disponibile, altrimenti una stringa normale.
func (e Env) propertyKey(key string) (sys.Value, error) {
	var r sys.Value
	st := sys.CreatePropertyKeyUTF8(e.raw, unsafe.StringData(key), uint(len(key)), &r)
	if st == sys.StatusUnavailable {
		st = sys.CreateStringUTF8(e.raw, unsafe.StringData(key), uint(len(key)), &r)
	}
	if st != sys.StatusOK {
		return nil, statusError(e.raw, st)
	}
	return r, nil
}

const (
	// stackStringBytes è la capacità del buffer sullo stack di GoString: le stringhe più corte si
	// leggono con un solo attraversamento di cgo.
	stackStringBytes = 256
	// utf8MaxCharBytes è la lunghezza massima di un carattere UTF-8.
	utf8MaxCharBytes = 4
)

// GoString legge una stringa JS come UTF-8. Fino a stackStringBytes-utf8MaxCharBytes-1 byte basta
// una chiamata a Node-API; l'unica allocazione è la stringa restituita. Un valore che non è una
// stringa dà un *Error con StatusStringExpected (non c'è coercizione: per quella si usa ToString).
func (v Value) GoString() (string, error) {
	var buf [stackStringBytes]byte
	var n uint
	if err := check(v.env, sys.GetValueStringUTF8(v.env, v.raw, &buf[0], uint(len(buf)), &n)); err != nil {
		return "", err
	}
	// Node scrive al massimo len(buf)-1 byte più il NUL e, se tronca, non spezza un carattere:
	// una stringa troncata occupa più di len(buf)-1-utf8MaxCharBytes byte. Sotto quella soglia la
	// stringa è completa; altrimenti si rilegge la lunghezza.
	if n < uint(len(buf))-1-utf8MaxCharBytes {
		return string(buf[:n]), nil
	}
	var total uint
	if err := check(v.env, sys.GetValueStringUTF8(v.env, v.raw, nil, 0, &total)); err != nil {
		return "", err
	}
	b := make([]byte, total+1)
	if err := check(v.env, sys.GetValueStringUTF8(v.env, v.raw, &b[0], uint(len(b)), &n)); err != nil {
		return "", err
	}
	return unsafe.String(&b[0], int(n)), nil
}

// UTF16 legge una stringa JS come unità UTF-16.
func (v Value) UTF16() ([]uint16, error) {
	var total uint
	if err := check(v.env, sys.GetValueStringUTF16(v.env, v.raw, nil, 0, &total)); err != nil {
		return nil, err
	}
	b := make([]uint16, total+1)
	var n uint
	if err := check(v.env, sys.GetValueStringUTF16(v.env, v.raw, &b[0], uint(len(b)), &n)); err != nil {
		return nil, err
	}
	return b[:n], nil
}

// Latin1 legge una stringa JS come ISO-8859-1: i caratteri oltre U+00FF vengono troncati al byte
// basso, come in Node-API.
func (v Value) Latin1() ([]byte, error) {
	var total uint
	if err := check(v.env, sys.GetValueStringLatin1(v.env, v.raw, nil, 0, &total)); err != nil {
		return nil, err
	}
	b := make([]byte, total+1)
	var n uint
	if err := check(v.env, sys.GetValueStringLatin1(v.env, v.raw, &b[0], uint(len(b)), &n)); err != nil {
		return nil, err
	}
	return b[:n], nil
}

// ---- symbol ----

// Symbol è un valore symbol.
type Symbol struct{ Value }

// NewSymbol crea un nuovo symbol con la descrizione data (Symbol(description)).
func (e Env) NewSymbol(description string) (Symbol, error) {
	d, err := e.NewString(description)
	if err != nil {
		return Symbol{}, err
	}
	var r sys.Value
	st := sys.CreateSymbol(e.raw, d.raw, &r)
	v, err := e.value(r, st)
	return Symbol{v}, err
}

// SymbolFor restituisce il symbol globale del registro (Symbol.for(key), Node-API 9).
func (e Env) SymbolFor(key string) (Symbol, error) {
	var r sys.Value
	st := sys.SymbolFor(e.raw, unsafe.StringData(key), uint(len(key)), &r)
	v, err := e.value(r, st)
	return Symbol{v}, err
}

// ---- bigint ----

// BigInt è un valore bigint.
type BigInt struct{ Value }

// NewBigIntInt64 crea un bigint da un int64.
func (e Env) NewBigIntInt64(i int64) (BigInt, error) {
	var r sys.Value
	st := sys.CreateBigIntInt64(e.raw, i, &r)
	v, err := e.value(r, st)
	return BigInt{v}, err
}

// NewBigIntUint64 crea un bigint da un uint64.
func (e Env) NewBigIntUint64(u uint64) (BigInt, error) {
	var r sys.Value
	st := sys.CreateBigIntUint64(e.raw, u, &r)
	v, err := e.value(r, st)
	return BigInt{v}, err
}

// NewBigIntWords crea un bigint da parole di 64 bit in ordine little-endian (la meno
// significativa per prima) e dal segno.
func (e Env) NewBigIntWords(negative bool, words []uint64) (BigInt, error) {
	sign := int32(0)
	if negative {
		sign = 1
	}
	var r sys.Value
	st := sys.CreateBigIntWords(e.raw, sign, uint(len(words)), unsafe.SliceData(words), &r)
	v, err := e.value(r, st)
	return BigInt{v}, err
}

// NewBigInt crea un bigint da un *big.Int.
func (e Env) NewBigInt(b *big.Int) (BigInt, error) {
	if b == nil {
		return BigInt{}, TypeError("", "NewBigInt: nil *big.Int")
	}
	return e.NewBigIntWords(b.Sign() < 0, bigToWords(b))
}

// Int64 legge il bigint come int64; lossless è false se il valore non ci sta (troncato).
func (b BigInt) Int64() (v int64, lossless bool, err error) {
	st := sys.GetValueBigIntInt64(b.env, b.raw, &v, &lossless)
	return v, lossless, check(b.env, st)
}

// Uint64 legge il bigint come uint64; lossless è false se il valore non ci sta (negativo o troppo
// grande).
func (b BigInt) Uint64() (v uint64, lossless bool, err error) {
	st := sys.GetValueBigIntUint64(b.env, b.raw, &v, &lossless)
	return v, lossless, check(b.env, st)
}

// Words restituisce il segno e le parole di 64 bit (little-endian) del bigint.
func (b BigInt) Words() (negative bool, words []uint64, err error) {
	var count uint
	if err := check(b.env, sys.GetValueBigIntWords(b.env, b.raw, nil, &count, nil)); err != nil {
		return false, nil, err
	}
	if count == 0 {
		return false, nil, nil
	}
	words = make([]uint64, count)
	var sign int32
	if err := check(b.env, sys.GetValueBigIntWords(b.env, b.raw, &sign, &count, &words[0])); err != nil {
		return false, nil, err
	}
	return sign != 0, words[:count], nil
}

// Big legge il bigint come *big.Int.
func (b BigInt) Big() (*big.Int, error) {
	negative, words, err := b.Words()
	if err != nil {
		return nil, err
	}
	r := wordsToBig(words)
	if negative {
		r.Neg(r)
	}
	return r, nil
}

const wordBytes = 8

func bigToWords(b *big.Int) []uint64 {
	be := new(big.Int).Abs(b).Bytes() // big-endian
	words := make([]uint64, (len(be)+wordBytes-1)/wordBytes)
	for i := range be {
		// byte i-esimo dal fondo → parola i/8, posizione i%8.
		byteFromEnd := be[len(be)-1-i]
		words[i/wordBytes] |= uint64(byteFromEnd) << (wordBytes * uint(i%wordBytes))
	}
	return words
}

func wordsToBig(words []uint64) *big.Int {
	be := make([]byte, len(words)*wordBytes)
	for i, w := range words {
		for j := 0; j < wordBytes; j++ {
			be[len(be)-1-(i*wordBytes+j)] = byte(w >> (wordBytes * uint(j)))
		}
	}
	return new(big.Int).SetBytes(be)
}

// ---- date ----

// Date è un oggetto Date.
type Date struct{ Object }

const nanosPerMilli = float64(time.Millisecond)

// NewDate crea una Date da t. Il valore di una Date JS è un numero intero di millisecondi: la parte
// frazionaria viene troncata da JS.
func (e Env) NewDate(t time.Time) (Date, error) {
	// UnixMilli e non UnixNano: UnixNano trabocca fuori dagli anni 1678-2262.
	subMilli := float64(t.Nanosecond()%int(time.Millisecond)) / nanosPerMilli
	return e.NewDateMillis(float64(t.UnixMilli()) + subMilli)
}

// NewDateMillis crea una Date dai millisecondi dall'epoch Unix (come new Date(ms)).
func (e Env) NewDateMillis(ms float64) (Date, error) {
	var r sys.Value
	st := sys.CreateDate(e.raw, ms, &r)
	v, err := e.value(r, st)
	return Date{Object{v}}, err
}

// Millis restituisce il valore della Date in millisecondi dall'epoch (NaN per una Date non valida).
func (d Date) Millis() (float64, error) {
	var ms float64
	st := sys.GetDateValue(d.env, d.raw, &ms)
	return ms, check(d.env, st)
}

// ErrInvalidDate è restituito da Date.Time per una Date non valida (new Date(NaN)).
var ErrInvalidDate = &Error{Kind: KindRangeError, Message: "invalid Date"}

// Time restituisce la Date come time.Time.
func (d Date) Time() (time.Time, error) {
	ms, err := d.Millis()
	if err != nil {
		return time.Time{}, err
	}
	if math.IsNaN(ms) {
		return time.Time{}, ErrInvalidDate
	}
	whole := math.Trunc(ms)
	frac := ms - whole
	return time.UnixMilli(int64(whole)).Add(time.Duration(frac * nanosPerMilli)), nil
}
