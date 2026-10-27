package main

import (
	"fmt"
	"strings"
	"unicode"
)

// baseKind classifica i tipi C di base (senza puntatori).
type baseKind int

const (
	kindHandle   baseKind = iota // puntatore opaco Node-API (napi_value, napi_ref, ...)
	kindCallback                 // puntatore a funzione C
	kindEnum                     // enum Node-API
	kindPrim                     // tipo numerico o bool
	kindMirror                   // struct con tipo Go specchio
	kindChar                     // char / char16_t: solo dietro puntatore
	kindVoid                     // void: solo dietro puntatore
	kindOpaque                   // struct opaca esterna (uv_loop_s): solo dietro puntatore
)

type baseType struct {
	kind   baseKind
	goType string // tipo Go
	cType  string // tipo C normalizzato da usare nel wrapper C (es. napi_env per node_api_basic_env)
}

var baseTypes = map[string]baseType{
	"napi_env":           {kindHandle, "Env", "napi_env"},
	"node_api_basic_env": {kindHandle, "Env", "napi_env"},
	"node_api_nogc_env":  {kindHandle, "Env", "napi_env"},

	"napi_value":                     {kindHandle, "Value", "napi_value"},
	"napi_ref":                       {kindHandle, "Ref", "napi_ref"},
	"napi_handle_scope":              {kindHandle, "HandleScope", "napi_handle_scope"},
	"napi_escapable_handle_scope":    {kindHandle, "EscapableHandleScope", "napi_escapable_handle_scope"},
	"napi_callback_info":             {kindHandle, "CallbackInfo", "napi_callback_info"},
	"napi_deferred":                  {kindHandle, "Deferred", "napi_deferred"},
	"napi_callback_scope":            {kindHandle, "CallbackScope", "napi_callback_scope"},
	"napi_async_context":             {kindHandle, "AsyncContext", "napi_async_context"},
	"napi_async_work":                {kindHandle, "AsyncWork", "napi_async_work"},
	"napi_threadsafe_function":       {kindHandle, "ThreadsafeFunction", "napi_threadsafe_function"},
	"napi_async_cleanup_hook_handle": {kindHandle, "AsyncCleanupHookHandle", "napi_async_cleanup_hook_handle"},

	"napi_callback":                    {kindCallback, "Callback", "napi_callback"},
	"napi_finalize":                    {kindCallback, "Finalize", "napi_finalize"},
	"node_api_basic_finalize":          {kindCallback, "Finalize", "napi_finalize"},
	"node_api_nogc_finalize":           {kindCallback, "Finalize", "napi_finalize"},
	"napi_threadsafe_function_call_js": {kindCallback, "ThreadsafeFunctionCallJS", "napi_threadsafe_function_call_js"},
	"napi_async_execute_callback":      {kindCallback, "AsyncExecuteCallback", "napi_async_execute_callback"},
	"napi_async_complete_callback":     {kindCallback, "AsyncCompleteCallback", "napi_async_complete_callback"},
	"napi_cleanup_hook":                {kindCallback, "CleanupHook", "napi_cleanup_hook"},
	"napi_async_cleanup_hook":          {kindCallback, "AsyncCleanupHook", "napi_async_cleanup_hook"},

	"bool":     {kindPrim, "bool", "bool"},
	"int":      {kindPrim, "int32", "int"},
	"int32_t":  {kindPrim, "int32", "int32_t"},
	"uint32_t": {kindPrim, "uint32", "uint32_t"},
	"int64_t":  {kindPrim, "int64", "int64_t"},
	"uint64_t": {kindPrim, "uint64", "uint64_t"},
	"double":   {kindPrim, "float64", "double"},
	"size_t":   {kindPrim, "uint", "size_t"},

	"char":             {kindChar, "byte", "char"},
	"char16_t":         {kindChar, "uint16", "char16_t"},
	"void":             {kindVoid, "", "void"},
	"struct uv_loop_s": {kindOpaque, "", "struct uv_loop_s"},
}

func init() {
	for c, n := range enumConfig {
		baseTypes[c] = baseType{kindEnum, n.goType, c}
	}
	for c, g := range mirrors {
		baseTypes[c] = baseType{kindMirror, g, c}
	}
}

// Ruolo di un parametro nel wrapper.
type role int

const (
	roleIn   role = iota // valore in ingresso
	roleWord             // void* opaco conservato da Node-API: uintptr in Go, cast in C
	rolePtr              // puntatore passato così com'è (buffer, array, inout)
	roleOut              // uscita restituita per valore nella struct del wrapper C
)

// Binding descrive come un parametro C viene esposto in Go e passato al wrapper C.
type Binding struct {
	Param
	Role    role
	GoName  string // nome del parametro Go
	GoType  string // tipo del parametro Go
	CParam  string // dichiarazione nel prototipo del wrapper C (vuota per le uscite)
	CArg    string // espressione C passata alla funzione Node-API
	ToC     string // espressione Go passata al wrapper C
	OutType string // tipo C del campo di uscita
	FromC   string // formato per convertire il campo di uscita in Go (%s = campo)
}

// FuncBinding è una funzione pronta per l'emissione.
type FuncBinding struct {
	Func
	GoName     string
	Params     []Binding
	Outs       []Binding // sottoinsieme di Params con Role == roleOut, in ordine
	NoCallback bool
	NoAlloc    bool
	NoEscape   bool
}

func splitPointer(ctype string) (base string, isConst bool, depth int) {
	t := ctype
	for strings.HasSuffix(t, "*") {
		depth++
		t = strings.TrimSuffix(t, "*")
	}
	t = strings.TrimSpace(t)
	if strings.HasPrefix(t, "const ") {
		isConst = true
		t = strings.TrimPrefix(t, "const ")
	}
	return t, isConst, depth
}

func bindParam(fn string, p Param) (Binding, error) {
	b := Binding{Param: p, GoName: goParamName(p.Name)}
	base, isConst, depth := splitPointer(p.CType)
	bt, ok := baseTypes[base]
	if !ok {
		return b, fmt.Errorf("%s: tipo C senza mappatura Go: %q", fn, p.CType)
	}
	override := paramOverrides[fn][p.Name]
	cName := "C." + strings.ReplaceAll(bt.cType, "struct ", "struct_")
	unknown := func() (Binding, error) {
		return b, fmt.Errorf("%s: combinazione di tipo non prevista: %q", fn, p.CType)
	}

	switch depth {
	case 0:
		switch bt.kind {
		case kindHandle, kindCallback, kindEnum, kindPrim:
			b.Role, b.GoType = roleIn, bt.goType
			b.CParam = bt.cType + " " + p.Name
			b.CArg = p.Name
			b.ToC = fmt.Sprintf("%s(%s)", cName, b.GoName)
		default:
			return unknown()
		}

	case 1:
		switch {
		case bt.kind == kindVoid && !isConst:
			// Dato opaco conservato da Node-API (data, hint, context, ...): mai un puntatore Go.
			b.Role, b.GoType = roleWord, "uintptr"
			b.CParam = "uintptr_t " + p.Name
			b.CArg = "(void*)" + p.Name
			b.ToC = fmt.Sprintf("C.uintptr_t(%s)", b.GoName)
		case bt.kind == kindVoid && isConst:
			b.Role, b.GoType = rolePtr, "unsafe.Pointer"
			b.CParam = "const void* " + p.Name
			b.CArg = p.Name
			b.ToC = b.GoName
		case bt.kind == kindChar:
			b.Role, b.GoType = rolePtr, "*"+bt.goType
			b.CParam = p.CType + " " + p.Name
			b.CArg = p.Name
			b.ToC = fmt.Sprintf("(*%s)(unsafe.Pointer(%s))", cName, b.GoName)
		case bt.kind == kindMirror && isConst:
			b.Role, b.GoType = rolePtr, "*"+bt.goType
			b.CParam = p.CType + " " + p.Name
			b.CArg = p.Name
			b.ToC = fmt.Sprintf("(*%s)(unsafe.Pointer(%s))", cName, b.GoName)
		case (bt.kind == kindHandle || bt.kind == kindEnum || bt.kind == kindPrim) && (isConst || override != ""):
			// Array in ingresso (const) oppure eccezione dichiarata in paramOverrides.
			b.Role, b.GoType = rolePtr, "*"+bt.goType
			b.CParam = normType(strings.Replace(p.CType, base, bt.cType, 1)) + " " + p.Name
			b.CArg = p.Name
			b.ToC = fmt.Sprintf("(*%s)(unsafe.Pointer(%s))", cName, b.GoName)
		case bt.kind == kindHandle || bt.kind == kindEnum || bt.kind == kindPrim:
			b.Role, b.GoType = roleOut, "*"+bt.goType
			b.OutType = bt.cType
			b.FromC = bt.goType + "(%s)"
		default:
			return unknown()
		}

	case 2:
		switch {
		case bt.kind == kindVoid && !isConst:
			b.Role, b.GoType = roleOut, "*unsafe.Pointer"
			b.OutType, b.FromC = "void*", "%s"
		case bt.kind == kindOpaque:
			b.Role, b.GoType = roleOut, "*unsafe.Pointer"
			b.OutType, b.FromC = bt.cType+"*", "unsafe.Pointer(%s)"
		case bt.kind == kindChar && isConst:
			b.Role, b.GoType = roleOut, "**"+bt.goType
			b.OutType = "const " + bt.cType + "*"
			b.FromC = "(*" + bt.goType + ")(unsafe.Pointer(%s))"
		case bt.kind == kindMirror && isConst:
			b.Role, b.GoType = roleOut, "**"+bt.goType
			b.OutType = "const " + bt.cType + "*"
			b.FromC = "(*" + bt.goType + ")(unsafe.Pointer(%s))"
		default:
			return unknown()
		}

	default:
		return unknown()
	}
	if override != "" && b.Role != rolePtr {
		return b, fmt.Errorf("%s: override %q non applicabile a %q", fn, override, p.Name)
	}
	if b.Role == roleOut && bt.kind == kindPrim && bt.goType == "bool" {
		b.FromC = "bool(%s)"
	}
	return b, nil
}

func bindFunc(f Func) (FuncBinding, error) {
	fb := FuncBinding{
		Func:       f,
		GoName:     goFuncName(f.Name),
		NoCallback: noCallback[f.Name],
		NoAlloc:    noAlloc[f.Name],
		NoEscape:   !escapes[f.Name],
	}
	if f.Ret == "void" && f.Name != "napi_fatal_error" {
		return fb, fmt.Errorf("%s: funzione void non prevista", f.Name)
	}
	seen := map[string]bool{}
	for _, p := range f.Params {
		b, err := bindParam(f.Name, p)
		if err != nil {
			return fb, err
		}
		if seen[b.GoName] {
			return fb, fmt.Errorf("%s: nome di parametro Go duplicato %q", f.Name, b.GoName)
		}
		seen[b.GoName] = true
		fb.Params = append(fb.Params, b)
		if b.Role == roleOut {
			fb.Outs = append(fb.Outs, b)
		}
	}
	for name := range paramOverrides[f.Name] {
		found := false
		for _, p := range f.Params {
			found = found || p.Name == name
		}
		if !found {
			return fb, fmt.Errorf("%s: override per il parametro inesistente %q", f.Name, name)
		}
	}
	return fb, nil
}

// validatePolicy controlla che ogni nome citato in policy.go esista.
func validatePolicy(funcs []Func, enums []Enum) error {
	known := map[string]bool{}
	for _, f := range funcs {
		known[f.Name] = true
	}
	var problems []string
	check := func(set map[string]bool, what string) {
		for n := range set {
			if !known[n] {
				problems = append(problems, fmt.Sprintf("%s: %q non è una funzione degli header", what, n))
			}
		}
	}
	check(noCallback, "noCallback")
	check(noAlloc, "noAlloc")
	check(escapes, "escapes")
	for n := range excluded {
		if !known[n] {
			problems = append(problems, fmt.Sprintf("excluded: %q non è una funzione degli header", n))
		}
	}
	for n := range paramOverrides {
		if !known[n] {
			problems = append(problems, fmt.Sprintf("paramOverrides: %q non è una funzione degli header", n))
		}
	}
	for n := range noAlloc {
		if !noCallback[n] {
			problems = append(problems, fmt.Sprintf("noAlloc: %q deve essere anche in noCallback", n))
		}
	}
	for n := range noCallback {
		if escapes[n] {
			problems = append(problems, fmt.Sprintf("%q è sia in noCallback sia in escapes", n))
		}
	}
	knownEnums := map[string]bool{}
	for _, e := range enums {
		knownEnums[e.CName] = true
		if _, ok := enumConfig[e.CName]; !ok {
			problems = append(problems, fmt.Sprintf("enum %q senza voce in enumConfig", e.CName))
		}
	}
	for n := range enumConfig {
		if !knownEnums[n] {
			problems = append(problems, fmt.Sprintf("enumConfig: %q non è un enum degli header", n))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("policy non valida:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}

// goFuncName: napi_create_int32 → CreateInt32, node_api_symbol_for → SymbolFor.
func goFuncName(c string) string {
	c = strings.TrimPrefix(c, "napi_")
	c = strings.TrimPrefix(c, "node_api_")
	return camel(c, true)
}

// goEnumName: napi_ok con strip "napi_" e prefix "Status" → StatusOK.
func goEnumName(cname string, n enumNaming) string {
	return n.prefix + camel(strings.TrimPrefix(cname, n.strip), true)
}

var goKeywords = setOf("break", "case", "chan", "const", "continue", "default", "defer", "else",
	"fallthrough", "for", "func", "go", "goto", "if", "import", "interface", "map", "package",
	"range", "return", "select", "struct", "switch", "type", "var")

// goParamName: finalize_cb → finalizeCb; parole chiave Go: func → fn.
func goParamName(c string) string {
	n := camel(c, false)
	if goKeywords[n] {
		switch n {
		case "func":
			return "fn"
		case "type":
			return "typ"
		default:
			return n + "_"
		}
	}
	// I nomi usati internamente dai wrapper non devono essere oscurati.
	if n == "gnWant" || n == "gnR" {
		return n + "_"
	}
	return n
}

func camel(snake string, upperFirst bool) string {
	var b strings.Builder
	for i, part := range strings.Split(snake, "_") {
		if part == "" {
			continue
		}
		if i == 0 && !upperFirst {
			b.WriteString(part)
			continue
		}
		if s, ok := initialisms[part]; ok {
			b.WriteString(s)
			continue
		}
		r := []rune(part)
		r[0] = unicode.ToUpper(r[0])
		b.WriteString(string(r))
	}
	return b.String()
}
