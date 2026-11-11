package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// e2eAddon is the annotated test addon: its generated files, checked in and compiled by the e2e
// tests, are the golden files of the generator.
const e2eAddon = "../../tests/e2e/addons/codegen"

func TestGeneratedFilesOfE2EAddonAreUpToDate(t *testing.T) {
	outs, err := generate(config{dir: e2eAddon, goFile: defaultGoFile, dtsFile: defaultDTSFile, jsFile: defaultJSFile, name: "codegen"})
	if err != nil {
		t.Fatal(err)
	}
	if len(outs) != 3 {
		t.Fatalf("got %d files, want 3", len(outs))
	}
	for _, o := range outs {
		cur, err := os.ReadFile(o.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(cur, o.content) {
			t.Errorf("%s is out of date: run go generate ./tests/e2e/addons/codegen", o.path)
		}
	}
}

// sourceHeader starts the packages of genSource: the imports the cases may use.
const sourceHeader = `package main

import (
	"context"
	"math/big"
	"time"

	"github.com/napi-bindings/napi-go/napi"
)

var (
	_ context.Context
	_ *big.Int
	_ time.Time
	_ napi.Env
)

func main() {}

`

// firstBodyLine is the line of the first line of a case body, after sourceHeader.
const firstBodyLine = 20

// genSource generates for a package made of one file, sourceHeader followed by body, and returns
// the Go file and the declarations.
func genSource(t *testing.T, body string) (goSrc, dts string, err error) {
	t.Helper()
	// Inside the module, so that the package resolves napi like the addons of the repository.
	dir, err := os.MkdirTemp("testdata", "case")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	src := sourceHeader + body
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	outs, err := generate(config{dir: dir, goFile: defaultGoFile, dtsFile: defaultDTSFile})
	if err != nil {
		return "", "", err
	}
	return string(outs[0].content), string(outs[1].content), nil
}

func TestMain(m *testing.M) {
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestErrors(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"channel parameter", "//napi:export\nfunc F(c chan int) {}", fmt.Sprintf("main.go:%d: func F: parameter 1: type chan int has no JS conversion", firstBodyLine+1)},
		{"function result", "//napi:export\nfunc F() func() { return nil }", "result: type func() has no JS conversion"},
		{"nested unsupported field", "type S struct{ A []map[string]complex128 }\n//napi:export\nfunc F(s S) {}", "field A: type complex128 has no JS conversion"},
		{"map key", "//napi:export\nfunc F(m map[float64]int) {}", "map keys must be strings or integers"},
		{"non-empty interface parameter", "//napi:export\nfunc F(e error) {}", "type error cannot be decoded from JS"},
		{"duplicate field names", "type S struct{ A int `napi:\"x\"`; B int `napi:\"x\"` }\n//napi:export\nfunc F(s S) {}", `fields A and B have the same JS name "x"`},
		{"variadic", "//napi:export\nfunc F(xs ...int) {}", "variadic functions are not supported"},
		{"too many parameters", "//napi:export\nfunc F(a, b, c, d, e, f, g, h, i int) {}", "9 parameters: at most 8"},
		{"second result", "//napi:export\nfunc F() (int, string) { return 0, \"\" }", "the second result must be error"},
		{"three results", "//napi:export\nfunc F() (int, int, error) { return 0, 0, nil }", "3 results"},
		{"generic function", "//napi:export\nfunc F[T any](v T) {}", "generic functions cannot be exported"},
		{"async without error", "//napi:export\nfunc F(ctx context.Context) int { return 0 }", "must return (R, error) or error"},
		{"async with a JS value", "//napi:export\nfunc F(ctx context.Context, f napi.Function) error { return nil }", "refers to JS values and cannot be used in a goroutine"},
		{"async with Env", "//napi:export\nfunc F(ctx context.Context, e napi.Env) error { return nil }", "cannot be used in a goroutine"},
		{"async result with a JS value", "//napi:export\nfunc F(ctx context.Context) (napi.Object, error) { return napi.Object{}, nil }", "result: type napi.Object refers to JS values"},
		{"unknown directive", "//napi:exprot\nfunc F() {}", "//napi:exprot cannot be used on a function"},
		{"directive on the wrong declaration", "//napi:class\nfunc F() {}", "//napi:class cannot be used on a function"},
		{"method of a plain type", "type T struct{}\n//napi:getter\nfunc (t *T) X() int { return 0 }", "method of a type that is not a //napi:class"},
		{"class by value", "//napi:class\ntype C struct{}\n//napi:export\nfunc F(c C) {}", "class C must be used as *C"},
		{"constructor result", "//napi:class\ntype C struct{}\n//napi:constructor\nfunc New() *C { return nil }", "must return (*T, error)"},
		{"two constructors", "//napi:class\ntype C struct{}\n//napi:constructor\nfunc A() (*C, error) { return nil, nil }\n//napi:constructor\nfunc B() (*C, error) { return nil, nil }", "class C has two constructors (A and B)"},
		{"async method", "//napi:class\ntype C struct{}\nfunc (c *C) M(ctx context.Context) error { return nil }", "only plain functions can be asynchronous"},
		{"duplicate exports", "//napi:export same\nfunc A() {}\n//napi:export same\nfunc B() {}", `duplicate export "same"`},
		{"duplicate members", "//napi:class\ntype C struct{}\nfunc (c *C) A() {}\n//napi:export a\nfunc (c *C) B() {}", `duplicate member of C "a"`},
		{"reserved export name", "//napi:export delete\nfunc F() {}", `"delete" is not a valid JS name`},
		{"setter without getter", "//napi:class\ntype C struct{}\n//napi:setter\nfunc (c *C) SetX(v int) {}", `property "x" has a setter but no //napi:getter`},
		{"setter type", "//napi:class\ntype C struct{}\n//napi:getter\nfunc (c *C) X() int { return 0 }\n//napi:setter\nfunc (c *C) SetX(v string) {}", "setter SetX: must have the form func (*T) SetX(v int)"},
		{"fallible getter only", "//napi:class\ntype C struct{}\n//napi:getter\nfunc (c *C) X() (int, error) { return 0, nil }\n//napi:setter\nfunc (c *C) SetX(v int) {}", "must both return an error, or neither"},
		{"getter with parameters", "//napi:class\ntype C struct{}\n//napi:getter\nfunc (c *C) X(i int) int { return 0 }", "getter X: must have the form"},
		{"static of an unknown class", "//napi:export Nope.make\nfunc F() {}", "Nope is not a //napi:class type"},
		{"object on a non-struct", "//napi:object\ntype T int", "//napi:object needs a struct type"},
		{"empty type override", "//napi:type\ntype T int", "//napi:type needs the TypeScript type"},
		{"class and interface with the same name", "//napi:class Thing\ntype C struct{}\n//napi:object Thing\ntype O struct{}", `duplicate TypeScript type "Thing"`},
		{"export of several names", "//napi:export\nconst A, B = 1, 2", "needs a declaration with one name"},
		{"package that does not compile", "//napi:export\nfunc F() int { return missing }", "the package does not compile"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := genSource(t, c.body)
			if err == nil {
				t.Fatalf("no error, want %q", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not contain %q", err, c.want)
			}
		})
	}
}

func TestTypeMapping(t *testing.T) {
	goSrc, dts, err := genSource(t, `
type Celsius float64
type Raw []byte
type Inner struct{ URLPath string; ID int }
type Tree struct{ Value int; Children []Tree }
type Custom struct{}
func (c *Custom) FromJS(napi.Value) error { return nil }

//napi:object Config
type config struct {
	Inner
	When  time.Time
	Big   *big.Int
	Any   any
	Val   napi.Value
	Either napi.Either[string, *int]
	Skip  int `+"`napi:\"-\"`"+`
	Dash  int `+"`napi:\"-,\"`"+`
	Bytes []byte
}

//napi:export
func Input(a int8, b uint64, c []byte, d *string, e [2]bool, f [9]int, g map[string][]float32, h Celsius) {}

//napi:export
func In2(r Raw, t Tree, c Custom, o napi.Object, fn napi.Function, fns []napi.Function, cfg config) {}

//napi:export
func Out() (map[int]*Tree, error) { return nil, nil }

//napi:export
func Out2() []byte { return nil }

//napi:export
func Out3() [][]byte { return nil }

//napi:export
func Named(class string, _ int, _ int) {}

//napi:export
func Later(ctx context.Context) error { return nil }

//napi:export
//napi:returns Promise<string[]>
func Custom2(env napi.Env) (napi.Promise, error) { return napi.Promise{}, nil }
`)
	if err != nil {
		t.Fatal(err)
	}
	wantDTS := []string{
		"export declare function input(a: number, b: number | bigint, c: Uint8Array | ArrayBuffer | DataView, d: string | null | undefined, e: [boolean, boolean], f: number[], g: Record<string, number[]>, h: number): void",
		"export declare function in2(r: number[], t: Tree, c: any, o: object, fn: (...args: any[]) => any, fns: Array<(...args: any[]) => any>, cfg: Config): void",
		"export declare function out(): Record<number, Tree | null>",
		"export declare function out2(): Buffer",
		"export declare function out3(): Buffer[]",
		"export declare function named(class_: string, arg1: number, arg2: number): void",
		"export declare function later(): Promise<void>",
		"export declare function custom2(): Promise<string[]>",
		"export interface Tree {\n  value: number\n  children: Tree[]\n}",
		"export interface Config {\n  urlPath: string\n  id: number\n  when: Date\n  big?: bigint | null\n  any?: any\n  val?: any\n  either?: string | number | null\n  \"-\": number\n  bytes: Uint8Array\n}",
	}
	for _, w := range wantDTS {
		if !strings.Contains(dts, w) {
			t.Errorf("declarations do not contain:\n%s\n\ngot:\n%s", w, dts)
		}
	}
	for _, w := range []string{`napi.Export("input", napi.Proc8(Input))`, `napi.Export("out", napi.FuncE0(Out))`, `napi.Export("later", napi.AsyncProc0(Later))`, `napi.Export("custom2", napi.FuncE1(Custom2))`} {
		if !strings.Contains(goSrc, w) {
			t.Errorf("Go source does not contain %s\n\ngot:\n%s", w, goSrc)
		}
	}
}

func TestLowerCamel(t *testing.T) {
	// The same cases as napi's own lowerCamel (struct field names): the two must agree.
	cases := map[string]string{
		"Name": "name", "UserID": "userID", "ID": "id", "URLPath": "urlPath", "HTTPServer": "httpServer",
		"X": "x", "A1": "a1", "ABC1": "abc1", "already": "already", "": "",
	}
	for in, want := range cases {
		if got := lowerCamel(in); got != want {
			t.Errorf("lowerCamel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCheckMode(t *testing.T) {
	dir, err := os.MkdirTemp("testdata", "check")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	src := "package main\n\n// F does nothing.\n//\n//napi:export\nfunc F() {}\n\nfunc main() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config{dir: dir, goFile: defaultGoFile, dtsFile: defaultDTSFile, jsFile: defaultJSFile}
	if err := run(cfg, true); err == nil || !strings.Contains(err.Error(), "run napi-go-gen") {
		t.Fatalf("check before generating: %v", err)
	}
	if err := run(cfg, false); err != nil {
		t.Fatal(err)
	}
	if err := run(cfg, true); err != nil {
		t.Fatalf("check after generating: %v", err)
	}
	// A second run reads the package together with its generated file.
	if err := run(cfg, false); err != nil {
		t.Fatalf("second run: %v", err)
	}
	js, err := os.ReadFile(filepath.Join(dir, defaultJSFile))
	if err != nil {
		t.Fatal(err)
	}
	if want := "require('./" + filepath.Base(dir) + ".node')"; !strings.Contains(string(js), want) {
		t.Errorf("entry point %q does not contain %s", js, want)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(strings.Replace(src, "func F()", "func G()", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(cfg, true); err == nil || !strings.Contains(err.Error(), "out of date") {
		t.Fatalf("check after a change: %v", err)
	}
	// The stale generated file refers to F, which no longer exists: generation still works.
	if err := run(cfg, false); err != nil {
		t.Fatalf("regeneration with a stale file: %v", err)
	}
}
