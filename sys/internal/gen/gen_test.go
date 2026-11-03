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

	// Functions listed in symbols.js of node-api-headers 1.9.0 (Node-API 1–10).
	symbolsInHeaders = 155
	// napi_module_register is excluded (deprecated registration entry point).
	exposedFuncs = symbolsInHeaders - 1
)

func TestStripCommentsAndPreprocessor(t *testing.T) {
	src := "a // line\nb /* multi-line\n block */ c\n#if X\n#define LONG \\\n  continued\nd\n#endif\n"
	got := strings.Fields(clean(src))
	want := []string{"a", "b", "c", "d"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("clean = %q, want %q", got, want)
	}
	if got := clean("x /* unclosed"); strings.TrimSpace(got) != "x" {
		t.Fatalf("unclosed comment: %q", got)
	}
	if got := clean(""); got != "" {
		t.Fatalf("empty input: %q", got)
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
			t.Fatalf("%q: %d params, want %d (%v)", c.in, len(got), len(c.want), got)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q[%d] = %+v, want %+v", c.in, i, got[i], c.want[i])
			}
		}
	}
	if _, err := parseParams("napi_env"); err == nil {
		t.Error("unnamed parameter must return an error")
	}
}

func TestParseFuncs(t *testing.T) {
	src := `
NAPI_EXTERN napi_status NAPI_CDECL
napi_create_int32(napi_env env,
                  int32_t value,   // comment
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
		t.Fatalf("%d functions, want 2", len(funcs))
	}
	if f := funcs[0]; f.Name != "napi_create_int32" || f.Ret != "napi_status" || len(f.Params) != 3 || f.NoReturn {
		t.Errorf("first function: %+v", f)
	}
	if f := funcs[1]; f.Name != "napi_fatal_error" || f.Ret != "void" || !f.NoReturn {
		t.Errorf("second function: %+v", f)
	}
	// A declaration with an unexpected shape must not be silently ignored.
	if _, err := parseFuncs("x.h", "NAPI_EXTERN int NAPI_CDECL strange(void);"); err == nil {
		t.Error("unrecognized NAPI_EXTERN declaration must return an error")
	}
	if funcs, err := parseFuncs("empty.h", ""); err != nil || len(funcs) != 0 {
		t.Errorf("empty header: %v, %v", funcs, err)
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
			t.Errorf("evalExpr(%q) = %d, %v; want %d", expr, got, err, want)
		}
	}
	for _, bad := range []string{"", "c", "(a", "a b", "a |", "1 + 2"} {
		if _, err := evalExpr(bad, env); err == nil {
			t.Errorf("evalExpr(%q): expected an error", bad)
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
		t.Fatalf("%d enums, want 2", len(enums))
	}
	want := []EnumMember{{"x_a", 0}, {"x_b", 8}, {"x_c", 9}, {"x_d", 8}}
	for i, m := range enums[0].Members {
		if m != want[i] {
			t.Errorf("member %d = %+v, want %+v", i, m, want[i])
		}
	}
	if _, err := parseEnums("typedef enum { z = unknown } z_t;"); err == nil {
		t.Error("unknown identifier must return an error")
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
module.exports = { v1, v2, 'ignored': 1 }`
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
			t.Errorf("%s: version %d, want %d", k, versions[k], v)
		}
	}
	if _, _, err := parseSymbols("const v2 = {\n'a'\n}\nconst v1 = {\n'b'\n}"); err == nil {
		t.Error("non-increasing versions must return an error")
	}
	if _, _, err := parseSymbols(""); err == nil {
		t.Error("file with no symbols must return an error")
	}
}

func TestAssignVersionsMismatch(t *testing.T) {
	funcs := []Func{{Name: "header_only"}}
	err := assignVersions(funcs, map[string]int{"symbols_only": 1})
	if err == nil || !strings.Contains(err.Error(), "header_only") || !strings.Contains(err.Error(), "symbols_only") {
		t.Fatalf("expected error mentioning both functions, got %v", err)
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
	t.Fatalf("%s not found", name)
	return FuncBinding{}
}

func TestRealHeaders(t *testing.T) {
	api := loadReal(t)
	if len(api.Funcs) != exposedFuncs {
		t.Errorf("%d exposed functions, want %d", len(api.Funcs), exposedFuncs)
	}
	if api.MaxVersion != 10 {
		t.Errorf("max version %d, want 10", api.MaxVersion)
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
			t.Errorf("%s: version %d, want %d", name, got, v)
		}
	}
	for _, f := range api.Funcs {
		if f.Name == "napi_module_register" {
			t.Error("napi_module_register must be excluded")
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
					t.Errorf("%s = %d, want %d", m.CName, m.Value, want)
				}
			}
		}
	}
	if found != len(enumValues) {
		t.Errorf("found %d enum values out of %d", found, len(enumValues))
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
			t.Errorf("%s: parameter %s not found", c.fn, c.param)
			continue
		}
		if b.Role != c.role || b.GoType != c.goType {
			t.Errorf("%s.%s: role %d type %s, want role %d type %s", c.fn, c.param, b.Role, b.GoType, c.role, c.goType)
		}
	}
}

// Functions that can execute JavaScript must never be marked nocallback: if they re-entered Go,
// the Go runtime would terminate the process.
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
			t.Errorf("%s must not be marked nocallback", name)
		}
	}
	if f := findFunc(t, api, "node_api_create_external_string_latin1"); f.NoEscape {
		t.Error("node_api_create_external_string_latin1 retains the buffer and must not be marked noescape")
	}
	if f := findFunc(t, api, "napi_get_value_string_utf8"); !f.NoCallback || !f.NoEscape {
		t.Error("napi_get_value_string_utf8 must be marked both noescape and nocallback (stack buffer)")
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
			t.Errorf("goFuncName(%s) = %s, want %s", c, got, want)
		}
	}
	params := map[string]string{"func": "fn", "type": "typ", "finalize_cb": "finalizeCb", "env": "env", "range": "range_"}
	for c, want := range params {
		if got := goParamName(c); got != want {
			t.Errorf("goParamName(%s) = %s, want %s", c, got, want)
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
	noCallback["napi_nonexistent"] = true
	defer delete(noCallback, "napi_nonexistent")
	if err := validatePolicy(funcs, api.Enums); err == nil || !strings.Contains(err.Error(), "napi_nonexistent") {
		t.Fatalf("unknown function name in policy must return an error, got %v", err)
	}
}

// The zz_* files in the repository must match the generator output.
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
			t.Errorf("%s is out of date: run go generate ./sys", f.Path)
		}
	}
}
