package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot is the napi-go checkout, used by the test projects through a replace directive.
const repoRoot = "../.."

// TestEndToEnd runs the life of an addon on this machine: new, build, its tests with node, gen
// -check, artifacts, prepublish, and loading it the way npm installs it (main package plus
// platform package, no local binary).
func TestEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds an addon: skipped with -short")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	host, err := hostTarget()
	if err != nil {
		t.Skip(err)
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "e2e-addon")
	cli := func(args ...string) string {
		t.Helper()
		out, stderr, code := runCLI(args...)
		if code != 0 {
			t.Fatalf("napi-go %s: exit code %d\n%s%s", strings.Join(args, " "), code, out, stderr)
		}
		return out
	}
	cli("new", "-replace", root, "-targets", host.name, dir)
	out := cli("build", "-C", dir)
	if !strings.Contains(out, "built "+host.binaryFile("e2e-addon")) {
		t.Fatalf("build output %q", out)
	}
	runNode := func(cwd string, args ...string) (string, error) {
		cmd := exec.Command(node, args...)
		cmd.Dir = cwd
		b, err := cmd.CombinedOutput()
		return string(b), err
	}
	if out, err := runNode(dir, "--test"); err != nil {
		t.Fatalf("tests of the new project: %v\n%s", err, out)
	}
	cli("gen", "-C", dir, "-check")
	// Edge cases of the project: targets that cannot be built, stale generated files.
	for _, bad := range []struct{ target, err string }{
		{"linux-x64-musl", "musl"},
		{"plan9-x64", "unknown target"},
		{",", "empty target name"},
	} {
		if _, stderr, code := runCLI("build", "-C", dir, "-no-gen", "-target", bad.target); code != exitFailure || !strings.Contains(stderr, bad.err) {
			t.Errorf("build -target %q: exit code %d, %s", bad.target, code, stderr)
		}
	}
	mainGo := filepath.Join(dir, "main.go")
	src := readFile(t, mainGo)
	writeFile(t, mainGo, strings.Replace(src, "func Sum(", "func Total(", 1))
	if _, stderr, code := runCLI("gen", "-C", dir, "-check"); code != exitFailure || !strings.Contains(stderr, "out of date") {
		t.Errorf("gen -check after a change: exit code %d, %s", code, stderr)
	}
	writeFile(t, mainGo, src)
	cli("artifacts", "-C", dir, "-d", ".")
	cli("prepublish", "-C", dir)

	// The layout of an npm install: the files of the main package and the platform package.
	consumer := t.TempDir()
	mainPkg := filepath.Join(consumer, "node_modules", "e2e-addon")
	for _, f := range []string{"index.js", "index.d.ts", packageJSONFile} {
		copyTestFile(t, filepath.Join(dir, f), filepath.Join(mainPkg, f))
	}
	const script = `const a = require('e2e-addon'); process.stdout.write(String(a.sum(20, 22)))`
	out, err = runNode(consumer, "-e", script)
	if err == nil || !strings.Contains(out, "e2e-addon-"+host.name+": package not installed") {
		t.Fatalf("without the platform package: %v\n%s", err, out)
	}
	platformDir := filepath.Join(dir, "npm", host.name)
	for _, f := range []string{packageJSONFile, host.binaryFile("e2e-addon")} {
		copyTestFile(t, filepath.Join(platformDir, f), filepath.Join(consumer, "node_modules", "e2e-addon-"+host.name, f))
	}
	if out, err := runNode(consumer, "-e", script); err != nil || out != "42" {
		t.Fatalf("with the platform package: %v\n%s", err, out)
	}
	// ES modules import the exports by name.
	const esm = `import { Counter } from 'e2e-addon'; process.stdout.write(String(new Counter(1).add(1)))`
	if out, err := runNode(consumer, "--input-type=module", "-e", esm); err != nil || out != "2" {
		t.Fatalf("named import: %v\n%s", err, out)
	}
	// The binary of another target is rejected by artifacts.
	other := host.name
	for _, tg := range allTargets {
		if tg.goos != host.goos {
			other = tg.name
			break
		}
	}
	copyTestFile(t, filepath.Join(dir, host.binaryFile("e2e-addon")), filepath.Join(dir, "wrong", "e2e-addon."+other+".node"))
	if _, stderr, code := runCLI("artifacts", "-C", dir, "-d", "wrong", "-target", other); code == 0 || !strings.Contains(stderr, "not a binary for "+other) {
		t.Errorf("artifacts with a binary of %s named for %s: exit code %d, %s", host.name, other, code, stderr)
	}
}

func copyTestFile(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatal(err)
	}
}
