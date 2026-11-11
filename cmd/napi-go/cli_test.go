package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/format"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// writeFile writes a file of a test, creating its directory.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// runCLI runs napi-go with args and returns its output and exit code.
func runCLI(args ...string) (stdout, stderr string, code int) {
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

func TestCommandLine(t *testing.T) {
	cases := []struct {
		args []string
		code int
		err  string
	}{
		{nil, exitUsage, "usage: napi-go"},
		{[]string{"help"}, 0, "usage: napi-go"},
		{[]string{"frobnicate"}, exitUsage, `unknown command "frobnicate"`},
		{[]string{"build", "-nope"}, exitUsage, "flag provided but not defined"},
		{[]string{"build", "-zig", "sometimes"}, exitFailure, `-zig "sometimes"`},
		{[]string{"build", "-zig", "always", "-cc", "gcc"}, exitFailure, "exclude each other"},
		{[]string{"new"}, exitUsage, "usage: napi-go new"},
		{[]string{"gen", "extra"}, exitUsage, "usage: napi-go gen"},
	}
	for _, c := range cases {
		// Flag errors are printed by the flag package on os.Stderr: only the code is checked
		// for them.
		_, stderr, code := runCLI(c.args...)
		if code != c.code {
			t.Errorf("%v: exit code %d, want %d (stderr %q)", c.args, code, c.code, stderr)
		}
		if code != exitUsage || len(c.args) == 0 || c.args[0] == "frobnicate" {
			if !strings.Contains(stderr, c.err) {
				t.Errorf("%v: stderr %q does not contain %q", c.args, stderr, c.err)
			}
		}
	}
	out, _, code := runCLI("version")
	if code != 0 || !strings.HasPrefix(out, "napi-go ") {
		t.Errorf("version: %q, exit code %d", out, code)
	}
}

func TestJSONObjectKeepsOrderAndText(t *testing.T) {
	const src = `{"name":"x","version":"1.0.0","scripts":{"b":"go tool napi-go build","a":"x && y <z>"},"z":[1,2],"name":"y"}`
	o := newJSONObject()
	if err := json.Unmarshal([]byte(src), o); err != nil {
		t.Fatal(err)
	}
	// A repeated key keeps its first position and its last value, as in JavaScript.
	if got := strings.Join(o.keys, ","); got != "name,version,scripts,z" {
		t.Fatalf("keys %s", got)
	}
	if err := o.set("version", "2.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := o.set("added", map[string]string{"k": "<&>"}); err != nil {
		t.Fatal(err)
	}
	o.remove("z")
	o.remove("missing")
	got, err := o.format()
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "name": "y",
  "version": "2.0.0",
  "scripts": {
    "b": "go tool napi-go build",
    "a": "x && y <z>"
  },
  "added": {
    "k": "<&>"
  }
}
`
	if string(got) != want {
		t.Errorf("format:\n%s\nwant:\n%s", got, want)
	}
	var name string
	if ok, err := o.get("name", &name); !ok || err != nil || name != "y" {
		t.Errorf("get name: %q %v %v", name, ok, err)
	}
	var n int
	if _, err := o.get("name", &n); err == nil {
		t.Error("get of a string into an int succeeded")
	}
	for _, bad := range []string{`[1]`, `"x"`, `{"a":}`, `{"a":1`} {
		if err := json.Unmarshal([]byte(bad), newJSONObject()); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
}

func TestWriteJSONObjectLeavesAnEqualFileUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.json")
	o := newJSONObject()
	if err := o.set("a", 1); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONObject(path, o); err != nil {
		t.Fatal(err)
	}
	st1, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Make the file read-only: a second write of the same content must not open it.
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	if err := writeJSONObject(path, o); err != nil {
		t.Fatalf("rewrite of an equal file: %v", err)
	}
	st2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !st2.ModTime().Equal(st1.ModTime()) {
		t.Error("the file was rewritten")
	}
}

func TestTargets(t *testing.T) {
	if _, err := lookupTarget("linux-x64-musl"); err == nil || !strings.Contains(err.Error(), "musl") {
		t.Errorf("musl target: %v", err)
	}
	if _, err := lookupTarget("linux-mips-gnu"); err == nil || !strings.Contains(err.Error(), "darwin-arm64") {
		t.Errorf("unknown target: %v (want the list of supported targets)", err)
	}
	for _, name := range defaultTargets {
		if _, err := lookupTarget(name); err != nil {
			t.Errorf("default target: %v", err)
		}
	}
	for _, tg := range allTargets {
		if _, ok := ciRunners[tg.name]; !ok {
			t.Errorf("%s has no CI runner in the template", tg.name)
		}
		if !strings.HasPrefix(tg.name, tg.platform+"-"+tg.arch) {
			t.Errorf("%s is not named after its platform and arch (the rule of the loader)", tg.name)
		}
	}
	host, err := hostTarget()
	if err != nil {
		t.Skip("no target for this machine:", err)
	}
	if host.isCross() {
		t.Error("the host target is a cross build")
	}
	got, err := resolveTargets("", nil)
	if err != nil || len(got) != 1 || got[0].name != host.name {
		t.Errorf("default target: %v %v", got, err)
	}
	got, err = resolveTargets(" darwin-arm64 ,all", []string{"linux-x64-gnu", "darwin-arm64"})
	names := make([]string, len(got))
	for i, tg := range got {
		names[i] = tg.name
	}
	if err != nil || strings.Join(names, ",") != "darwin-arm64,linux-x64-gnu" {
		t.Errorf("resolve with all and duplicates: %v %v", names, err)
	}
	if _, err := resolveTargets("all", nil); err == nil {
		t.Error("all with no project targets: no error")
	}
	if _, err := resolveTargets("linux-x64-gnu,bogus", nil); err == nil {
		t.Error("unknown target in a list: no error")
	}
}

func TestLoadProject(t *testing.T) {
	dir := t.TempDir()
	p, err := loadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.pkg != nil || p.cfg.BinaryName != filepath.Base(dir) || p.cfg.GoPackage != "." || !slices.Equal(p.cfg.Targets, defaultTargets) {
		t.Errorf("bare package: %+v", p.cfg)
	}
	if err := p.requirePackageJSON("artifacts"); err == nil {
		t.Error("requirePackageJSON without package.json: no error")
	}
	writeFile(t, filepath.Join(dir, packageJSONFile), `{"name":"@scope/fast-thing","version":"1.2.3","napiGo":{"goPackage":"go","codegen":false}}`)
	if p, err = loadProject(dir); err != nil {
		t.Fatal(err)
	}
	if p.cfg.BinaryName != "fast-thing" || p.goPackageDir() != filepath.Join(dir, "go") || p.codegenEnabled() {
		t.Errorf("scoped package: %+v", p.cfg)
	}
	if cfg := p.codegenConfig(); cfg.PackageName != "@scope/fast-thing" || cfg.JSFile != filepath.Join(dir, "index.js") {
		t.Errorf("codegen config: %+v", cfg)
	}
	bad := []struct{ json, err string }{
		{`{"name":"x","napiGo":{"binaryName":"a b"}}`, "binaryName"},
		{`{"name":"x","napiGo":{"targets":["linux-x64-musl"]}}`, "musl"},
		{`{"name":"x","napiGo":{"npmDir":"` + filepath.ToSlash(filepath.Join(dir, "abs")) + `"}}`, "relative"},
		{`{"name":"x","napiGo":{"targets":"linux-x64-gnu"}}`, "napiGo"},
		{`{"name":1}`, "name"},
		{`{"name":`, "package.json"},
	}
	for _, b := range bad {
		writeFile(t, filepath.Join(dir, packageJSONFile), b.json)
		if _, err := loadProject(dir); err == nil || !strings.Contains(err.Error(), b.err) {
			t.Errorf("%s: error %v, want %q", b.json, err, b.err)
		}
	}
	writeFile(t, filepath.Join(dir, packageJSONFile), `{"version":"1.0.0"}`)
	if p, err = loadProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := p.requirePackageJSON("prepublish"); err == nil || !strings.Contains(err.Error(), "no name") {
		t.Errorf("package.json without name: %v", err)
	}
}

func TestFindProject(t *testing.T) {
	root := t.TempDir()
	goDir := filepath.Join(root, "native")
	writeFile(t, filepath.Join(root, packageJSONFile), `{"name":"addon","version":"1.0.0","napiGo":{"goPackage":"native"}}`)
	writeFile(t, filepath.Join(goDir, "main.go"), "package main\n")
	// From the Go package (go generate): the project above it.
	p, err := findProject(goDir)
	if err != nil || p.dir != root {
		t.Fatalf("from the Go package: %v %v", p, err)
	}
	// A directory that is not the Go package of the project above: a bare package.
	other := filepath.Join(root, "other")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if p, err = findProject(other); err != nil || p.pkg != nil || p.dir != other {
		t.Fatalf("unrelated directory: %v %v", p, err)
	}
	// A package.json without napiGo above is not a napi-go project, even if it is invalid for it.
	writeFile(t, filepath.Join(root, packageJSONFile), `{"name":"Not Valid As Binary"}`)
	if p, err = findProject(goDir); err != nil || p.pkg != nil {
		t.Fatalf("package.json without napiGo: %v %v", p, err)
	}
}

// stubLookPath makes zig available or not for the duration of a test.
func stubLookPath(t *testing.T, haveZig bool) {
	t.Helper()
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	lookPath = func(file string) (string, error) {
		if file == "zig" && !haveZig {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + file, nil
	}
}

// envValue returns the value of key in env, or "".
func envValue(env []string, key string) string {
	v := ""
	for _, kv := range env {
		if k, val, ok := strings.Cut(kv, "="); ok && k == key {
			v = val
		}
	}
	return v
}

func TestToolchain(t *testing.T) {
	host, err := hostTarget()
	if err != nil {
		t.Skip(err)
	}
	// A target with another OS than the host: always a cross build.
	cross, err := lookupTarget(map[bool]string{true: "linux-x64-gnu", false: "win32-x64-gnu"}[runtime.GOOS == "windows"])
	if err != nil {
		t.Fatal(err)
	}
	stubLookPath(t, true)
	tc, err := toolchainFor(cross, buildOptions{zig: zigAuto})
	if err != nil {
		t.Fatal(err)
	}
	tc.cleanup()
	if cc := envValue(tc.env, "CC"); cc != "zig cc -target "+cross.zig || !tc.zig {
		t.Errorf("cross build: CC %q", cc)
	}
	if envValue(tc.env, "GOOS") != cross.goos || envValue(tc.env, "GOARCH") != cross.goarch || envValue(tc.env, "CGO_ENABLED") != "1" {
		t.Errorf("cross build env: %v", tc.env)
	}
	if tc, err := toolchainFor(cross, buildOptions{zig: zigAuto, cc: "my-cross-gcc"}); err != nil || envValue(tc.env, "CC") != "my-cross-gcc" {
		t.Errorf("-cc in a cross build: %v %v", tc, err)
	}
	if _, err := toolchainFor(cross, buildOptions{zig: zigNever}); err == nil {
		t.Error("cross build with -zig never and no -cc: no error")
	}
	if tc, err := toolchainFor(host, buildOptions{zig: zigAuto}); err != nil || envValue(tc.env, "CC") != "" {
		t.Errorf("native debug build: %v %v", tc, err)
	}
	if tc, err := toolchainFor(host, buildOptions{zig: zigAlways}); err != nil || !tc.zig {
		t.Errorf("native build with -zig always: %v %v", tc, err)
	}
	if host.goos == "linux" {
		if tc, err := toolchainFor(host, buildOptions{zig: zigAuto, release: true}); err != nil || !tc.zig {
			t.Errorf("Linux release build with zig available: %v %v", tc, err)
		}
	}

	stubLookPath(t, false)
	if _, err := toolchainFor(cross, buildOptions{zig: zigAuto}); err == nil || !strings.Contains(err.Error(), "needs zig") {
		t.Errorf("cross build without zig: %v", err)
	}
	if host.goos == "linux" {
		tc, err := toolchainFor(host, buildOptions{zig: zigAuto, release: true})
		if err != nil || tc.zig || len(tc.warnings) != 1 || !strings.Contains(tc.warnings[0], "glibc") {
			t.Errorf("Linux release build without zig: %+v %v", tc, err)
		}
	}
}

func TestDarwinStubs(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("the stubs are for builds on other systems")
	}
	stubLookPath(t, true)
	tg, err := lookupTarget("darwin-arm64")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CGO_LDFLAGS", "-lfoo")
	tc, err := toolchainFor(tg, buildOptions{zig: zigAuto})
	if err != nil {
		t.Fatal(err)
	}
	ldflags := envValue(tc.env, "CGO_LDFLAGS")
	fields := strings.Fields(ldflags)
	if len(fields) != 3 || fields[0] != "-lfoo" || !strings.HasPrefix(fields[1], "-L") || fields[2] != "-F"+fields[1][2:] {
		t.Fatalf("CGO_LDFLAGS %q: want the flags of the environment, then -L and -F of the stubs", ldflags)
	}
	dir := fields[1][2:]
	if !strings.Contains(readFile(t, filepath.Join(dir, "libresolv.tbd")), "_res_9_ninit") {
		t.Error("libresolv stub without res_9_ninit")
	}
	if !strings.Contains(readFile(t, filepath.Join(dir, "CoreFoundation.framework", "CoreFoundation.tbd")), "CoreFoundation.framework/Versions/A/CoreFoundation") {
		t.Error("CoreFoundation stub without its install name")
	}
	tc.cleanup()
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stub directory left after cleanup: %v", err)
	}
}

func TestGoBuildArgs(t *testing.T) {
	linux, err := lookupTarget("linux-arm64-gnu")
	if err != nil {
		t.Fatal(err)
	}
	win, err := lookupTarget("win32-x64-gnu")
	if err != nil {
		t.Fatal(err)
	}
	zig := &toolchain{zig: true}
	o := buildOptions{release: true, tags: "a,b", ldflags: "-X main.v=1", extra: []string{"-race"}}
	got := strings.Join(goBuildArgs(linux, o, zig, "out.node", "native"), " ")
	want := "build -buildmode=c-shared -trimpath -tags=a,b -ldflags=-s -w -extldflags=-Wl,--strip-all -X main.v=1 -o out.node -race ./native"
	if got != want {
		t.Errorf("release Linux build with zig:\n got %s\nwant %s", got, want)
	}
	got = strings.Join(goBuildArgs(win, buildOptions{release: true}, zig, "o.node", "."), " ")
	if want := "build -buildmode=c-shared -trimpath -ldflags=-s -w -o o.node ."; got != want {
		t.Errorf("release Windows build (no --strip-all, PE):\n got %s\nwant %s", got, want)
	}
	got = strings.Join(goBuildArgs(linux, buildOptions{}, &toolchain{}, "o.node", "."), " ")
	if want := "build -buildmode=c-shared -trimpath -o o.node ."; got != want {
		t.Errorf("debug build:\n got %s\nwant %s", got, want)
	}
}

func TestCheckBinaryRejectsOtherFiles(t *testing.T) {
	garbage := filepath.Join(t.TempDir(), "x.node")
	writeFile(t, garbage, "not a binary")
	for _, tg := range allTargets {
		if err := checkBinary(garbage, tg); err == nil {
			t.Errorf("%s: garbage accepted", tg.name)
		}
	}
	// The test executable is not a shared library, and is for one platform only.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tg := range allTargets {
		if err := checkBinary(exe, tg); err == nil {
			t.Errorf("%s: the test executable accepted as a shared library", tg.name)
		}
	}
}

func TestNewWritesTheProject(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "My-Addon")
	out, stderr, code := runCLI("new", "-no-install", "-name", "@acme/my-addon", "-targets", "linux-x64-gnu,win32-arm64-gnu", dir)
	if code != 0 {
		t.Fatalf("exit code %d: %s", code, stderr)
	}
	if !strings.Contains(out, "created @acme/my-addon") {
		t.Errorf("output %q", out)
	}
	for _, f := range []string{"go.mod", "main.go", packageJSONFile, "test/index.test.mjs", ".gitignore", "README.md", ".github/workflows/CI.yml"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Error(err)
		}
	}
	src := readFile(t, filepath.Join(dir, "main.go"))
	if formatted, err := format.Source([]byte(src)); err != nil || string(formatted) != src {
		t.Errorf("main.go is not gofmt-formatted (%v)", err)
	}
	if !strings.HasPrefix(src, "// Command my-addon ") {
		t.Errorf("main.go starts with %q", src[:40])
	}
	if got := readFile(t, filepath.Join(dir, "go.mod")); got != "module my-addon\n\ngo "+goVersion+"\n" {
		t.Errorf("go.mod %q", got)
	}
	p, err := loadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.name != "@acme/my-addon" || p.cfg.BinaryName != "my-addon" || !slices.Equal(p.cfg.Targets, []string{"linux-x64-gnu", "win32-arm64-gnu"}) {
		t.Errorf("package.json: %s %+v", p.name, p.cfg)
	}
	var access map[string]string
	if ok, err := p.pkg.get("publishConfig", &access); !ok || err != nil || access["access"] != "public" {
		t.Errorf("scoped package without publishConfig.access public: %v %v", access, err)
	}
	ci := readFile(t, filepath.Join(dir, ".github/workflows/CI.yml"))
	for _, want := range []string{"target: linux-x64-gnu\n            runner: ubuntu-latest", "target: win32-arm64-gnu\n            runner: windows-11-arm"} {
		if !strings.Contains(ci, want) {
			t.Errorf("CI.yml lacks %q", want)
		}
	}
	if strings.Contains(ci, "darwin") || strings.Contains(ci, "[[") {
		t.Error("CI.yml has a target not asked for, or an unexpanded template action")
	}
	if !strings.Contains(readFile(t, filepath.Join(dir, ".gitignore")), "/npm/*/*.node") {
		t.Error(".gitignore does not ignore the binaries of the platform packages")
	}
	// The directory is no longer empty.
	if _, stderr, code := runCLI("new", "-no-install", dir); code == 0 || !strings.Contains(stderr, "not empty") {
		t.Errorf("new in a non-empty directory: exit code %d, %s", code, stderr)
	}
}

func TestNewRejectsBadNames(t *testing.T) {
	base := t.TempDir()
	cases := []struct{ args []string }{
		{[]string{"-name", "Has Space"}},
		{[]string{"-name", "UPPER"}},
		{[]string{"-name", "@scope/"}},
		{[]string{"-name", strings.Repeat("a", maxNpmNameLength+1)}},
		{[]string{"-module", "bad path"}},
		{[]string{"-targets", "linux-x64-musl"}},
	}
	for i, c := range cases {
		args := append([]string{"new", "-no-install"}, c.args...)
		args = append(args, filepath.Join(base, strings.Repeat("d", i+1)))
		if _, stderr, code := runCLI(args...); code != exitFailure {
			t.Errorf("%v: exit code %d (%s)", c.args, code, stderr)
		}
	}
}

func TestArtifactsErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, packageJSONFile), `{"name":"demo","version":"1.0.0","napiGo":{"targets":["linux-x64-gnu","darwin-arm64"]}}`)
	writeFile(t, filepath.Join(dir, "artifacts", "a", "demo.linux-x64-gnu.node"), "x")
	_, stderr, code := runCLI("artifacts", "-C", dir, "-target", "darwin-arm64")
	if code != exitFailure || !strings.Contains(stderr, "demo.darwin-arm64.node") {
		t.Errorf("missing binary: exit code %d, %s", code, stderr)
	}
	// The binary of the wrong format is rejected before it is copied.
	_, stderr, code = runCLI("artifacts", "-C", dir, "-target", "linux-x64-gnu")
	if code != exitFailure || !strings.Contains(stderr, "not a binary for linux-x64-gnu") {
		t.Errorf("binary of the wrong format: exit code %d, %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "npm", "linux-x64-gnu", "demo.linux-x64-gnu.node")); err == nil {
		t.Error("a rejected binary was copied")
	}
	writeFile(t, filepath.Join(dir, "artifacts", "b", "demo.linux-x64-gnu.node"), "x")
	_, stderr, code = runCLI("artifacts", "-C", dir, "-target", "linux-x64-gnu")
	if code != exitFailure || !strings.Contains(stderr, "found twice") {
		t.Errorf("duplicate binary: exit code %d, %s", code, stderr)
	}
	// Copies in the platform packages and in node_modules are not candidates.
	if err := os.RemoveAll(filepath.Join(dir, "artifacts", "b")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "npm", "linux-x64-gnu", "demo.linux-x64-gnu.node"), "x")
	writeFile(t, filepath.Join(dir, "node_modules", "demo-linux-x64-gnu", "demo.linux-x64-gnu.node"), "x")
	found, err := findBinaries(mustLoad(t, dir), dir, []target{mustTarget(t, "linux-x64-gnu")})
	if err != nil || len(found) != 1 || found["linux-x64-gnu"] != filepath.Join(dir, "artifacts", "a", "demo.linux-x64-gnu.node") {
		t.Errorf("found %v %v", found, err)
	}
	if _, stderr, code := runCLI("artifacts", "-C", t.TempDir()); code != exitFailure || !strings.Contains(stderr, packageJSONFile) {
		t.Errorf("without package.json: exit code %d, %s", code, stderr)
	}
}

func mustLoad(t *testing.T, dir string) *project {
	t.Helper()
	p, err := loadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustTarget(t *testing.T, name string) target {
	t.Helper()
	tg, err := lookupTarget(name)
	if err != nil {
		t.Fatal(err)
	}
	return tg
}

// stubNpm records the npm commands of a test; view answers published for the specs in published.
func stubNpm(t *testing.T, published ...string) *[]string {
	t.Helper()
	orig := npmCommand
	t.Cleanup(func() { npmCommand = orig })
	var calls []string
	npmCommand = func(dir string, args ...string) ([]byte, error) {
		calls = append(calls, filepath.Base(dir)+": "+strings.Join(args, " "))
		if args[0] == "view" {
			if slices.Contains(published, args[1]) {
				return []byte(args[1][strings.LastIndexByte(args[1], '@')+1:] + "\n"), nil
			}
			return nil, errors.New("E404")
		}
		return nil, nil
	}
	return &calls
}

func TestPrepublish(t *testing.T) {
	dir := t.TempDir()
	const pkg = `{
  "name": "@acme/demo",
  "version": "2.0.0",
  "license": "MIT",
  "optionalDependencies": {
    "left-pad": "^1.0.0",
    "@acme/demo-win32-x64-gnu": "1.0.0"
  },
  "napiGo": {
    "targets": ["linux-x64-gnu", "darwin-arm64"]
  }
}
`
	writeFile(t, filepath.Join(dir, packageJSONFile), pkg)
	_, stderr, code := runCLI("prepublish", "-C", dir)
	if code != exitFailure || !strings.Contains(stderr, "run napi-go artifacts") {
		t.Fatalf("missing binaries: exit code %d, %s", code, stderr)
	}
	for _, tg := range []string{"linux-x64-gnu", "darwin-arm64"} {
		writeFile(t, filepath.Join(dir, "npm", tg, "demo."+tg+".node"), "x")
	}
	// A platform package of an older version, with a field of its own.
	writeFile(t, filepath.Join(dir, "npm", "darwin-arm64", packageJSONFile), `{"name":"old","version":"1.0.0","keywords":["kept"],"libc":["glibc"]}`)
	if _, stderr, code := runCLI("prepublish", "-C", dir); code != 0 {
		t.Fatalf("exit code %d: %s", code, stderr)
	}
	p := mustLoad(t, dir)
	var deps map[string]string
	if _, err := p.pkg.get("optionalDependencies", &deps); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"left-pad": "^1.0.0", "@acme/demo-linux-x64-gnu": "2.0.0", "@acme/demo-darwin-arm64": "2.0.0"}
	if len(deps) != len(want) {
		t.Errorf("optionalDependencies %v, want %v", deps, want)
	}
	for k, v := range want {
		if deps[k] != v {
			t.Errorf("optionalDependencies[%s] = %q, want %q", k, deps[k], v)
		}
	}
	if !strings.HasPrefix(readFile(t, filepath.Join(dir, packageJSONFile)), "{\n  \"name\": \"@acme/demo\",\n  \"version\": \"2.0.0\",\n  \"license\"") {
		t.Error("package.json keys reordered")
	}
	darwin := mustLoad(t, filepath.Join(dir, "npm", "darwin-arm64"))
	var meta struct {
		Name, Version, Main, License string
		OS, CPU, Libc, Keywords      []string
	}
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(darwin.dir, packageJSONFile))), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Name != "@acme/demo-darwin-arm64" || meta.Version != "2.0.0" || meta.Main != "demo.darwin-arm64.node" || meta.License != "MIT" ||
		!slices.Equal(meta.OS, []string{"darwin"}) || !slices.Equal(meta.CPU, []string{"arm64"}) || meta.Libc != nil || !slices.Equal(meta.Keywords, []string{"kept"}) {
		t.Errorf("platform package: %+v", meta)
	}
	if _, err := os.Stat(filepath.Join(darwin.dir, "README.md")); err != nil {
		t.Error(err)
	}

	calls := stubNpm(t, "@acme/demo-linux-x64-gnu@2.0.0")
	if _, stderr, code := runCLI("prepublish", "-C", dir, "-publish", "-tag", "next", "-dry-run"); code != 0 {
		t.Fatalf("publish: exit code %d: %s", code, stderr)
	}
	wantCalls := []string{
		"linux-x64-gnu: view @acme/demo-linux-x64-gnu@2.0.0 version",
		"darwin-arm64: view @acme/demo-darwin-arm64@2.0.0 version",
		"darwin-arm64: publish --tag next --dry-run --access public",
	}
	if !slices.Equal(*calls, wantCalls) {
		t.Errorf("npm calls:\n%s\nwant:\n%s", strings.Join(*calls, "\n"), strings.Join(wantCalls, "\n"))
	}
}
