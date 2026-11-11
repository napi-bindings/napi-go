package codegen

// types.go — the Go types the napi conversions accept (mirroring napi/conv.go) and their
// TypeScript counterparts.

import (
	"go/types"
	"reflect"
	"strconv"
	"strings"
	"unicode"
)

// napiValueTS maps the napi value types, passed to and from JS as they are, to TypeScript.
var napiValueTS = map[string]string{
	"Value":       "any",
	"Object":      "object",
	"Function":    "(...args: any[]) => any",
	"Array":       "any[]",
	"String":      "string",
	"Symbol":      "symbol",
	"BigInt":      "bigint",
	"Date":        "Date",
	"Promise":     "Promise<any>",
	"External":    "unknown",
	"Buffer":      "Buffer",
	"ArrayBuffer": "ArrayBuffer",
	"TypedArray":  "NodeJS.TypedArray",
	"DataView":    "DataView",
}

// tsKind is where a type appears, which changes how some types are written.
type tsKind int

const (
	tsParam  tsKind = iota // a parameter: every accepted JS type is listed
	tsResult               // a result
	tsIn                   // nested in a parameter
	tsOut                  // nested in a result
	tsField                // an interface field or property: used in both directions
)

func (k tsKind) nested() tsKind {
	switch k {
	case tsParam:
		return tsIn
	case tsResult:
		return tsOut
	}
	return k
}

// maxTuple is the longest Go array written as a TypeScript tuple; longer ones are T[].
const maxTuple = 8

// ts returns the TypeScript type of t. Types that cannot be converted are reported by check.
func (g *analyzer) ts(t types.Type, k tsKind) string {
	t = types.Unalias(t)
	if n, ok := t.(*types.Named); ok {
		obj := n.Origin().Obj()
		if o, ok := g.overrides[obj]; ok {
			return o
		}
		switch {
		case isNamed(n, "math/big", "Int"):
			return "bigint"
		case isNamed(n, "time", "Time"):
			return "Date"
		case obj.Pkg() != nil && obj.Pkg().Path() == napiPath && obj.Name() == "Either":
			return union(g.ts(n.TypeArgs().At(0), k.nested()), g.ts(n.TypeArgs().At(1), k.nested()))
		case obj.Pkg() != nil && obj.Pkg().Path() == napiPath && napiValueTS[obj.Name()] != "":
			return napiValueTS[obj.Name()]
		case g.custom(n, g.fromJS) || g.custom(n, g.toJS):
			return "any" // FromJS/ToJS: only the author knows the shape (//napi:type)
		}
		if g.classes[obj] != nil {
			return g.classes[obj].jsName // check reports the missing pointer
		}
		switch u := n.Underlying().(type) {
		case *types.Struct:
			if obj.Pkg() == g.pkg && n.TypeArgs().Len() == 0 && obj.Parent() == g.pkg.Scope() {
				return g.object(obj).name
			}
			return g.inlineStruct(u)
		case *types.Slice:
			// Only []byte itself is a Buffer: a named slice of bytes is an array of numbers.
			return array(g.ts(u.Elem(), k.nested()))
		}
		return g.ts(n.Underlying(), k)
	}
	switch u := t.(type) {
	case *types.Basic:
		switch {
		case u.Info()&types.IsBoolean != 0:
			return "boolean"
		case u.Info()&types.IsString != 0:
			return "string"
		case u.Kind() == types.UntypedNil:
			return "null"
		case k == tsParam && is64Bit(u.Kind()):
			return "number | bigint"
		}
		return "number"
	case *types.Pointer:
		var inner string
		if cls := g.classOf(u); cls != nil {
			inner = cls.jsName
		} else {
			inner = g.ts(u.Elem(), k.nested())
		}
		if k == tsParam {
			return union(inner, "null", "undefined")
		}
		return union(inner, "null")
	case *types.Slice:
		if types.Identical(u.Elem(), types.Typ[types.Uint8]) {
			switch k {
			case tsParam:
				return "Uint8Array | ArrayBuffer | DataView"
			case tsResult, tsOut:
				return "Buffer"
			}
			return "Uint8Array"
		}
		return array(g.ts(u.Elem(), k.nested()))
	case *types.Array:
		elem := g.ts(u.Elem(), k.nested())
		if u.Len() > maxTuple {
			return array(elem)
		}
		return "[" + strings.TrimSuffix(strings.Repeat(elem+", ", int(u.Len())), ", ") + "]"
	case *types.Map:
		key := "string"
		if b, ok := u.Key().Underlying().(*types.Basic); ok && b.Info()&types.IsInteger != 0 {
			key = "number"
		}
		return "Record<" + key + ", " + g.ts(u.Elem(), k.nested()) + ">"
	case *types.Struct:
		return g.inlineStruct(u)
	}
	return "any"
}

// is64Bit reports the kinds declared as 64-bit integers, for which a BigInt parameter is listed
// (int and uint accept one too, but are written as number).
func is64Bit(k types.BasicKind) bool {
	switch k {
	case types.Int64, types.Uint64:
		return true
	}
	return false
}

// union joins TypeScript types with |, parenthesizing function types.
func union(parts ...string) string {
	seen := map[string]bool{}
	var out []string
	for _, p := range parts {
		if strings.Contains(p, "=>") {
			p = "(" + p + ")"
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return strings.Join(out, " | ")
}

// array returns the TypeScript array of elem.
func array(elem string) string {
	if strings.ContainsAny(elem, "|&") || strings.Contains(elem, "=>") {
		return "Array<" + elem + ">"
	}
	return elem + "[]"
}

// custom reports whether *T implements iface (napi.FromJS or napi.ToJS).
func (g *analyzer) custom(n *types.Named, iface *types.Interface) bool {
	if _, isIface := n.Underlying().(*types.Interface); isIface {
		return false
	}
	return types.Implements(types.NewPointer(n), iface)
}

// ---- structs ----

// structField is a struct field converted as a property, as napi does (collectFields).
type structField struct {
	name      string
	v         *types.Var
	omitEmpty bool
}

// fields lists the converted fields of st: exported fields, embedded structs flattened, names
// from the napi tag, then the json tag, then the Go name in lowerCamelCase.
func (g *analyzer) fields(st *types.Struct, seen []*types.Struct) (out []structField, problem string) {
	for _, s := range seen {
		if s == st {
			return nil, ""
		}
	}
	seen = append(seen, st)
	names := map[string]string{}
	for i := 0; i < st.NumFields(); i++ {
		v := st.Field(i)
		tag := reflect.StructTag(st.Tag(i))
		raw, tagged := tag.Lookup("napi")
		if !tagged {
			raw, tagged = tag.Lookup("json")
		}
		name, opts, hasOpts := strings.Cut(raw, ",")
		if tagged && name == "-" && !hasOpts {
			continue
		}
		if inner, isStruct := v.Type().Underlying().(*types.Struct); v.Embedded() && !tagged && isStruct {
			if _, isPtr := types.Unalias(v.Type()).(*types.Pointer); !isPtr {
				fs, problem := g.fields(inner, seen)
				if problem != "" {
					return nil, problem
				}
				for _, f := range fs {
					if _, dup := names[f.name]; !dup {
						names[f.name] = f.v.Name()
						out = append(out, f)
					}
				}
				continue
			}
		}
		if !v.Exported() {
			continue
		}
		if name == "" {
			name = lowerCamel(v.Name())
		}
		if prev, dup := names[name]; dup {
			return nil, "fields " + prev + " and " + v.Name() + " have the same JS name " + strconv.Quote(name)
		}
		names[name] = v.Name()
		out = append(out, structField{name: name, v: v, omitEmpty: strings.Contains(opts, "omitempty")})
	}
	return out, ""
}

// optional reports whether a field of type t may be undefined in JS.
func (g *analyzer) optional(t types.Type) bool {
	t = types.Unalias(t)
	switch u := t.(type) {
	case *types.Pointer:
		return true
	case *types.Interface:
		return true
	case *types.Named:
		if isNamed(u, napiPath, "Value") {
			return true
		}
		if obj := u.Origin().Obj(); obj.Pkg() != nil && obj.Pkg().Path() == napiPath && obj.Name() == "Either" {
			return g.optional(u.TypeArgs().At(0)) || g.optional(u.TypeArgs().At(1))
		}
		if g.custom(u, g.fromJS) {
			return false
		}
		_, isIface := u.Underlying().(*types.Interface)
		return isIface
	}
	return false
}

func (g *analyzer) fieldModels(st *types.Struct) []fieldModel {
	fs, _ := g.fields(st, nil) // problems are reported by check
	out := make([]fieldModel, 0, len(fs))
	for _, f := range fs {
		out = append(out, fieldModel{name: f.name, ts: g.ts(f.v.Type(), tsField), doc: g.fieldDocs[f.v],
			optional: f.omitEmpty || g.optional(f.v.Type())})
	}
	return out
}

// object returns the interface emitted for the struct type obj, creating it on first use.
func (g *analyzer) object(obj *types.TypeName) *objectModel {
	if o := g.objects[obj]; o != nil {
		return o
	}
	o := &objectModel{pos: obj.Pos(), name: orDefault(g.objNames[obj], obj.Name()), doc: g.typeDocs[obj]}
	g.objects[obj] = o // before the fields: the type may be recursive
	g.claim(g.tsNames, "TypeScript type", o.name, o.pos)
	o.fields = g.fieldModels(obj.Type().Underlying().(*types.Struct))
	return o
}

func (g *analyzer) inlineStruct(st *types.Struct) string {
	fs := g.fieldModels(st)
	if len(fs) == 0 {
		return "{}"
	}
	parts := make([]string, len(fs))
	for i, f := range fs {
		parts[i] = propName(f.name) + map[bool]string{true: "?", false: ""}[f.optional] + ": " + f.ts
	}
	return "{ " + strings.Join(parts, "; ") + " }"
}

// ---- validation ----

// check reports why t cannot be decoded from JS (dec), encoded to JS (enc) or used in an
// asynchronous function (async), or "" if it can. It follows the rules of napi/conv.go.
func (g *analyzer) check(t types.Type, dec, enc, async bool, seen map[types.Type]bool) string {
	t = types.Unalias(t)
	no := func(why string) string { return "type " + g.typeString(t) + " " + why }
	if n, ok := t.(*types.Named); ok {
		if seen[n] {
			return ""
		}
		seen[n] = true
		obj := n.Origin().Obj()
		if obj.Pkg() != nil && obj.Pkg().Path() == napiPath {
			switch {
			case obj.Name() == "Either":
				for i := 0; i < 2; i++ {
					if why := g.check(n.TypeArgs().At(i), dec, enc, async, seen); why != "" {
						return why
					}
				}
				return ""
			case napiValueTS[obj.Name()] != "":
				if async {
					return no("refers to JS values and cannot be used in a goroutine")
				}
				return ""
			case obj.Name() == "Env" || obj.Name() == "CallContext":
				return no("can only be a parameter of the function itself")
			}
		}
		if isNamed(n, "math/big", "Int") || isNamed(n, "time", "Time") {
			return ""
		}
		if g.classes[obj] != nil {
			return "class " + obj.Name() + " must be used as *" + obj.Name()
		}
		dec = dec && !g.custom(n, g.fromJS)
		enc = enc && !g.custom(n, g.toJS)
		if !dec && !enc {
			return ""
		}
		why := g.check(n.Underlying(), dec, enc, async, seen)
		// A problem of the underlying type itself is reported with the name of the type.
		if rest, ok := strings.CutPrefix(why, "type "+g.typeString(n.Underlying())+" "); ok {
			return no(rest)
		}
		return why
	}
	switch u := t.(type) {
	case *types.Basic:
		if u.Info()&types.IsComplex != 0 || u.Kind() == types.UnsafePointer || u.Kind() == types.Invalid {
			return no("has no JS conversion")
		}
	case *types.Pointer:
		if g.classOf(u) != nil {
			return ""
		}
		return g.check(u.Elem(), dec, enc, async, seen)
	case *types.Slice:
		return g.check(u.Elem(), dec, enc, async, seen)
	case *types.Array:
		return g.check(u.Elem(), dec, enc, async, seen)
	case *types.Map:
		if b, ok := u.Key().Underlying().(*types.Basic); !ok || b.Info()&(types.IsString|types.IsInteger) == 0 {
			return no("has no JS conversion: map keys must be strings or integers")
		}
		return g.check(u.Elem(), dec, enc, async, seen)
	case *types.Struct:
		fs, problem := g.fields(u, nil)
		if problem != "" {
			return no("has " + problem)
		}
		for _, f := range fs {
			if why := g.check(f.v.Type(), dec, enc, async, seen); why != "" {
				return "field " + f.v.Name() + ": " + why
			}
		}
	case *types.Interface:
		if dec && !u.Empty() {
			return no("cannot be decoded from JS (only the empty interface can)")
		}
	default:
		return no("has no JS conversion")
	}
	return ""
}

// ---- names ----

// lowerCamel converts a Go identifier to the JS convention, like napi does for struct fields:
// UserID → userID, ID → id, URLPath → urlPath.
func lowerCamel(s string) string {
	r := []rune(s)
	n := 0
	for n < len(r) && r[n] >= 'A' && r[n] <= 'Z' {
		n++
	}
	if n == 0 {
		return s
	}
	if n > 1 && n < len(r) && r[n] >= 'a' && r[n] <= 'z' {
		n--
	}
	for i := 0; i < n; i++ {
		r[i] += 'a' - 'A'
	}
	return string(r)
}

// validJSName reports whether s can name an export (a JS identifier that is not a reserved
// word) or, with the prefix "static " or member set, a class member (any identifier).
func validJSName(s string, member bool) bool {
	if rest, ok := strings.CutPrefix(s, "static "); ok {
		s, member = rest, true
	}
	return s != "" && propName(s) == s && (member || !tsReserved[s])
}

// propName quotes a property name that is not an identifier.
func propName(s string) string {
	for i, r := range s {
		if !(r == '_' || r == '$' || unicode.IsLetter(r) || i > 0 && unicode.IsDigit(r)) || r > unicode.MaxASCII {
			return strconv.Quote(s)
		}
	}
	if s == "" {
		return `""`
	}
	return s
}

// tsReserved are the words that cannot name an export or a parameter in TypeScript.
var tsReserved = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`break case catch class const continue debugger default delete do
		else enum export extends false finally for function if import in instanceof new null return
		super switch this throw true try typeof var void while with yield let static implements
		interface package private protected public await arguments eval`) {
		tsReserved[w] = true
	}
}
