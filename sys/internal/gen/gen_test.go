package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	sysDir     = "../.."
	includeDir = "../../include"

	// Funzioni in symbols.js di node-api-headers 1.9.0 (Node-API 1–10).
	symbolsInHeaders = 155
	// napi_module_register è esclusa (registrazione deprecata).
	exposedFuncs = symbolsInHeaders - 1
)

func TestStripCommentsAndPreprocessor(t *testing.T) {
	src := "a // riga\nb /* blocco\n su più righe */ c\n#if X\n#define LONG \\\n  continua\nd\n#endif\n"
	got := strings.Fields(clean(src))
	want := []string{"a", "b", "c", "d"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("clean = %q, atteso %q", got, want)
	}
	if got := clean("x /* non chiuso"); strings.TrimSpace(got) != "x" {
		t.Fatalf("commento non chiuso: %q", got)
	}
	if got := clean(""); got != "" {
		t.Fatalf("input vuoto: %q", got)
	}
}

func TestParseParams(t *testing.T) {
	cases := []struct {
		in   string
		want []Param
	}{
		{"", nil},
		{"void", nil},
		{"napi_env env", []Param{{"napi_env", "env"}}},
		{"const char * str, size_t  length", []Param{{"const char*", "str"}, {"size_t", "length"}}},
		{"const napi_extended_error_info **result", []Param{{"const napi_extended_error_info**", "result"}}},
		{"struct uv_loop_s** loop", []Param{{"struct uv_loop_s**", "loop"}}},
	}
	for _, c := range cases {
		got, err := parseParams(c.in)
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if len(got) != len(c.want) {
			t.Fatalf("%q: %d parametri, attesi %d (%v)", c.in, len(got), len(c.want), got)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q[%d] = %+v, atteso %+v", c.in, i, got[i], c.want[i])
			}
		}
	}
	if _, err := parseParams("napi_env"); err == nil {
		t.Error("un parametro senza nome deve essere un errore")
	}
}

func TestParseFuncs(t *testing.T) {
	src := `
NAPI_EXTERN napi_status NAPI_CDECL
napi_create_int32(napi_env env,
                  int32_t value,   // commento
                  napi_value* result);
#if NAPI_VERSION >= 9
NAPI_EXTERN NAPI_NO_RETURN void NAPI_CDECL napi_fatal_error(const char* location, size_t len);
#endif
`
	funcs, err := parseFuncs("x.h", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(funcs) != 2 {
		t.Fatalf("%d funzioni, attese 2", len(funcs))
	}
	if f := funcs[0]; f.Name != "napi_create_int32" || f.Ret != "napi_status" || len(f.Params) != 3 || f.NoReturn {
		t.Errorf("prima funzione: %+v", f)
	}
	if f := funcs[1]; f.Name != "napi_fatal_error" || f.Ret != "void" || !f.NoReturn {
		t.Errorf("seconda funzione: %+v", f)
	}
	// Una dichiarazione con una forma non prevista non deve sparire in silenzio.
	if _, err := parseFuncs("x.h", "NAPI_EXTERN int NAPI_CDECL strana(void);"); err == nil {
		t.Error("una NAPI_EXTERN non riconosciuta deve essere un errore")
	}
	if funcs, err := parseFuncs("vuoto.h", ""); err != nil || len(funcs) != 0 {
		t.Errorf("header vuoto: %v, %v", funcs, err)
	}
}

func TestEvalExpr(t *testing.T) {
	env := map[string]int64{"a": 1, "b": 2}
	ok := map[string]int64{
		"0":            0,
		"7":            7,
		"0x10":         16,
		"1 << 10":      1024,
		"a | b":        3,
		"(a | b) << 2": 12,
		"a | b | 1<<3": 11,
	}
	for expr, want := range ok {
		got, err := evalExpr(expr, env)
		if err != nil || got != want {
			t.Errorf("evalExpr(%q) = %d, %v; atteso %d", expr, got, err, want)
		}
	}
	for _, bad := range []string{"", "c", "(a", "a b", "a |", "1 + 2"} {
		if _, err := evalExpr(bad, env); err == nil {
			t.Errorf("evalExpr(%q): atteso un errore", bad)
		}
	}
}

func TestParseEnums(t *testing.T) {
	src := `typedef enum {
  x_a,
  x_b = 1 << 3,
  x_c,
  x_d = x_a | x_b,
} x_t;
typedef enum { y_only } y_t;`
	enums, err := parseEnums(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(enums) != 2 {
		t.Fatalf("%d enum, attesi 2", len(enums))
	}
	want := []EnumMember{{"x_a", 0}, {"x_b", 8}, {"x_c", 9}, {"x_d", 8}}
	for i, m := range enums[0].Members {
		if m != want[i] {
			t.Errorf("membro %d = %+v, atteso %+v", i, m, want[i])
		}
	}
	if _, err := parseEnums("typedef enum { z = sconosciuto } z_t;"); err == nil {
		t.Error("un identificatore sconosciuto deve essere un errore")
	}
}

func TestParseSymbols(t *testing.T) {
	src := `const v1 = {
    js_native_api_symbols: ['a', 'b'],
    node_api_symbols: ['n']
}
const v2 = {
    js_native_api_symbols: [...v1.js_native_api_symbols, 'c'],
    node_api_symbols: [...v1.node_api_symbols]
}
module.exports = { v1, v2, 'ignorato': 1 }`
	versions, maxVersion, err := parseSymbols(src)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"a": 1, "b": 1, "n": 1, "c": 2}
	if len(versions) != len(want) || maxVersion != 2 {
		t.Fatalf("versions = %v, max %d", versions, maxVersion)
	}
	for k, v := range want {
		if versions[k] != v {
			t.Errorf("%s: versione %d, attesa %d", k, versions[k], v)
		}
	}
	if _, _, err := parseSymbols("const v2 = {\n'a'\n}\nconst v1 = {\n'b'\n}"); err == nil {
		t.Error("versioni non crescenti devono essere un errore")
	}
	if _, _, err := parseSymbols(""); err == nil {
		t.Error("un file senza simboli deve essere un errore")
	}
}

func TestAssignVersionsMismatch(t *testing.T) {
	funcs := []Func{{Name: "solo_header"}}
	err := assignVersions(funcs, map[string]int{"solo_symbols": 1})
	if err == nil || !strings.Contains(err.Error(), "solo_header") || !strings.Contains(err.Error(), "solo_symbols") {
		t.Fatalf("atteso un errore che citi entrambe le funzioni, ottenuto %v", err)
	}
}

func loadReal(t *testing.T) API {
	t.Helper()
	api, err := load(includeDir)
	if err != nil {
		t.Fatal(err)
	}
	return api
}

func findFunc(t *testing.T, api API, name string) FuncBinding {
	t.Helper()
	for _, f := range api.Funcs {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("%s non trovata", name)
	return FuncBinding{}
}

func TestRealHeaders(t *testing.T) {
	api := loadReal(t)
	if len(api.Funcs) != exposedFuncs {
		t.Errorf("%d funzioni esposte, attese %d", len(api.Funcs), exposedFuncs)
	}
	if api.MaxVersion != 10 {
		t.Errorf("versione massima %d, attesa 10", api.MaxVersion)
	}
	versions := map[string]int{
		"napi_create_int32":                 1,
		"napi_get_uv_event_loop":            2,
		"napi_add_env_cleanup_hook":         3,
		"napi_create_threadsafe_function":   4,
		"napi_add_finalizer":                5,
		"napi_create_bigint_words":          6,
		"napi_detach_arraybuffer":           7,
		"napi_type_tag_object":              8,
		"node_api_symbol_for":               9,
		"node_api_create_property_key_utf8": 10,
	}
	for name, v := range versions {
		if got := findFunc(t, api, name).Version; got != v {
			t.Errorf("%s: versione %d, attesa %d", name, got, v)
		}
	}
	for _, f := range api.Funcs {
		if f.Name == "napi_module_register" {
			t.Error("napi_module_register deve essere esclusa")
		}
	}
	enumValues := map[string]int64{
		"napi_static":             1024,
		"napi_default_jsproperty": 7,
		"napi_float16_array":      11,
		"napi_cannot_run_js":      23,
		"napi_key_skip_symbols":   16,
		"napi_tsfn_blocking":      1,
	}
	found := 0
	for _, e := range api.Enums {
		for _, m := range e.Members {
			if want, ok := enumValues[m.CName]; ok {
				found++
				if m.Value != want {
					t.Errorf("%s = %d, atteso %d", m.CName, m.Value, want)
				}
			}
		}
	}
	if found != len(enumValues) {
		t.Errorf("trovati %d valori enum su %d", found, len(enumValues))
	}
}

func TestParamRoles(t *testing.T) {
	api := loadReal(t)
	cases := []struct {
		fn, param string
		role      role
		goType    string
	}{
		{"napi_get_cb_info", "argc", rolePtr, "*uint"},
		{"napi_get_cb_info", "argv", rolePtr, "*Value"},
		{"napi_get_cb_info", "this_arg", roleOut, "*Value"},
		{"napi_get_cb_info", "data", roleOut, "*unsafe.Pointer"},
		{"napi_wrap", "native_object", roleWord, "uintptr"},
		{"napi_wrap", "result", roleOut, "*Ref"},
		{"napi_create_function", "data", roleWord, "uintptr"},
		{"napi_create_buffer_copy", "data", rolePtr, "unsafe.Pointer"},
		{"napi_create_buffer_copy", "result_data", roleOut, "*unsafe.Pointer"},
		{"napi_get_value_string_utf8", "buf", rolePtr, "*byte"},
		{"napi_get_value_string_utf8", "result", roleOut, "*uint"},
		{"napi_get_value_string_utf16", "buf", rolePtr, "*uint16"},
		{"napi_get_last_error_info", "result", roleOut, "**ExtendedErrorInfo"},
		{"napi_get_node_version", "version", roleOut, "**NodeVersion"},
		{"node_api_get_module_file_name", "result", roleOut, "**byte"},
		{"napi_get_uv_event_loop", "loop", roleOut, "*unsafe.Pointer"},
		{"napi_call_function", "argv", rolePtr, "*Value"},
		{"napi_define_properties", "properties", rolePtr, "*PropertyDescriptor"},
		{"napi_get_value_bigint_words", "word_count", rolePtr, "*uint"},
		{"napi_get_value_bigint_words", "words", rolePtr, "*uint64"},
		{"napi_get_value_bigint_words", "sign_bit", roleOut, "*int32"},
		{"napi_get_typedarray_info", "type", roleOut, "*TypedArrayType"},
		{"napi_get_value_bool", "result", roleOut, "*bool"},
	}
	for _, c := range cases {
		f := findFunc(t, api, c.fn)
		var b *Binding
		for i := range f.Params {
			if f.Params[i].Name == c.param {
				b = &f.Params[i]
			}
		}
		if b == nil {
			t.Errorf("%s: parametro %s non trovato", c.fn, c.param)
			continue
		}
		if b.Role != c.role || b.GoType != c.goType {
			t.Errorf("%s.%s: ruolo %d tipo %s, atteso ruolo %d tipo %s", c.fn, c.param, b.Role, b.GoType, c.role, c.goType)
		}
	}
}

// Le funzioni che possono eseguire JavaScript non devono mai avere nocallback: se rientrassero
// in Go il runtime terminerebbe il processo.
func TestNoCallbackExcludesJSRunningFunctions(t *testing.T) {
	api := loadReal(t)
	runsJS := []string{
		"napi_get_property", "napi_set_property", "napi_has_property", "napi_delete_property",
		"napi_get_named_property", "napi_set_named_property", "napi_get_element", "napi_set_element",
		"napi_get_property_names", "napi_get_all_property_names", "napi_get_prototype",
		"napi_coerce_to_string", "napi_coerce_to_number", "napi_instanceof", "napi_define_properties",
		"napi_object_freeze", "napi_object_seal", "napi_call_function", "napi_new_instance",
		"napi_make_callback", "napi_run_script", "napi_resolve_deferred", "napi_reject_deferred",
		"napi_fatal_exception", "napi_close_callback_scope",
		"node_api_create_external_string_latin1", "node_api_create_external_string_utf16",
	}
	for _, name := range runsJS {
		if f := findFunc(t, api, name); f.NoCallback || f.NoAlloc {
			t.Errorf("%s non deve essere nocallback", name)
		}
	}
	if f := findFunc(t, api, "node_api_create_external_string_latin1"); f.NoEscape {
		t.Error("node_api_create_external_string_latin1 conserva il buffer: non deve essere noescape")
	}
	if f := findFunc(t, api, "napi_get_value_string_utf8"); !f.NoCallback || !f.NoEscape {
		t.Error("napi_get_value_string_utf8 deve essere noescape e nocallback (buffer sullo stack)")
	}
}

func TestGoNames(t *testing.T) {
	names := map[string]string{
		"napi_create_int32":                "CreateInt32",
		"node_api_symbol_for":              "SymbolFor",
		"napi_get_uv_event_loop":           "GetUVEventLoop",
		"napi_get_value_bigint_int64":      "GetValueBigIntInt64",
		"napi_create_external_arraybuffer": "CreateExternalArrayBuffer",
		"napi_get_value_string_utf8":       "GetValueStringUTF8",
		"napi_typeof":                      "Typeof",
	}
	for c, want := range names {
		if got := goFuncName(c); got != want {
			t.Errorf("goFuncName(%s) = %s, atteso %s", c, got, want)
		}
	}
	params := map[string]string{"func": "fn", "type": "typ", "finalize_cb": "finalizeCb", "env": "env", "range": "range_"}
	for c, want := range params {
		if got := goParamName(c); got != want {
			t.Errorf("goParamName(%s) = %s, atteso %s", c, got, want)
		}
	}
	if got := goEnumName("napi_ok", enumConfig["napi_status"]); got != "StatusOK" {
		t.Errorf("napi_ok → %s", got)
	}
}

func TestPolicyValidation(t *testing.T) {
	api := loadReal(t)
	var funcs []Func
	for _, f := range api.Funcs {
		funcs = append(funcs, f.Func)
	}
	noCallback["napi_inesistente"] = true
	defer delete(noCallback, "napi_inesistente")
	if err := validatePolicy(funcs, api.Enums); err == nil || !strings.Contains(err.Error(), "napi_inesistente") {
		t.Fatalf("un nome sconosciuto nella policy deve essere un errore, ottenuto %v", err)
	}
}

// I file zz_* nel repository devono corrispondere all'output del generatore.
func TestGeneratedFilesUpToDate(t *testing.T) {
	files, err := emitAll(loadReal(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		cur, err := os.ReadFile(filepath.Join(sysDir, f.Path))
		if err != nil {
			t.Errorf("%s: %v", f.Path, err)
			continue
		}
		if !bytes.Equal(cur, f.Content) {
			t.Errorf("%s non è aggiornato: eseguire go generate ./sys", f.Path)
		}
	}
}
