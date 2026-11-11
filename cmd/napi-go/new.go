package main

// new.go — napi-go new: creates an addon project from the files in template/.

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

//go:embed template
var templates embed.FS

// templateFiles maps the templates to the files they create in the project.
var templateFiles = []struct{ tmpl, file string }{
	{"main.go.tmpl", "main.go"},
	{"index.test.mjs.tmpl", "test/index.test.mjs"},
	{"gitignore.tmpl", ".gitignore"},
	{"README.md.tmpl", "README.md"},
	{"CI.yml.tmpl", ".github/workflows/CI.yml"},
}

// goVersion is the go directive of a new project: the minimum Go version of napi-go.
const goVersion = "1.24"

// ciRunners are the GitHub-hosted runners of the CI template, per target.
var ciRunners = map[string]string{
	"linux-x64-gnu":   "ubuntu-latest",
	"linux-arm64-gnu": "ubuntu-24.04-arm",
	"darwin-x64":      "macos-15-intel",
	"darwin-arm64":    "macos-latest",
	"win32-x64-gnu":   "windows-latest",
	"win32-arm64-gnu": "windows-11-arm",
}

// templateData is the data of the templates.
type templateData struct {
	Name      string // npm package
	Binary    string
	NpmDir    string
	GoVersion string
	Glibc     string
	Targets   []templateTarget
}

type templateTarget struct{ Name, Runner string }

// newOptions are the flags of new.
type newOptions struct {
	name      string // npm package
	module    string // Go module path
	version   string // version of napi-go to require
	replace   string // local napi-go directory, for a replace directive
	targets   []string
	noInstall bool
}

// npmName matches valid npm package names (lowercase, optionally scoped).
var npmName = regexp.MustCompile(`^(@[a-z0-9][a-z0-9._~-]*/)?[a-z0-9][a-z0-9._~-]*$`)

// maxNpmNameLength is the limit of npm for package names.
const maxNpmNameLength = 214

func runNew(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet(stderr, "new", "<directory>")
	var o newOptions
	fs.StringVar(&o.name, "name", "", "npm package name (default: the directory name)")
	fs.StringVar(&o.module, "module", "", "Go module path (default: the package name without scope)")
	fs.StringVar(&o.version, "napi-go-version", "", "version of napi-go to require (default: the version of this command, or latest)")
	fs.StringVar(&o.replace, "replace", "", "local napi-go directory to use instead of a published version (replace directive)")
	targets := fs.String("targets", strings.Join(defaultTargets, ","), "comma-separated targets built by CI and published")
	fs.BoolVar(&o.noInstall, "no-install", false, "only write the files: do not run go get, napi-go gen and go mod tidy")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errUsage
	}
	dir, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return err
	}
	if o.name == "" {
		o.name = strings.ToLower(filepath.Base(dir))
	}
	if len(o.name) > maxNpmNameLength || !npmName.MatchString(o.name) {
		return fmt.Errorf("invalid npm package name %q: use lowercase letters, digits, '-', '.', '_' (or -name)", o.name)
	}
	binary := defaultBinaryName(o.name, dir)
	if !validBinaryName(binary) {
		return fmt.Errorf("package name %q does not make a valid binary name", o.name)
	}
	if o.module == "" {
		o.module = binary
	}
	if strings.ContainsAny(o.module, " \t\n\"'`") {
		return fmt.Errorf("invalid Go module path %q", o.module)
	}
	for _, t := range strings.Split(*targets, ",") {
		t = strings.TrimSpace(t)
		if _, err := lookupTarget(t); err != nil {
			return err
		}
		o.targets = append(o.targets, t)
	}
	if o.version == "" {
		o.version = version()
		if o.version == develVersion || o.version == "" {
			o.version = "latest"
		}
	}
	if o.replace != "" {
		if o.replace, err = filepath.Abs(o.replace); err != nil {
			return err
		}
	}
	if err := checkEmptyDir(dir); err != nil {
		return err
	}
	if err := writeProject(dir, binary, o); err != nil {
		return err
	}
	if !o.noInstall {
		if err := installProject(dir, o, stdout); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(stdout, "created %s\n\n  cd %s\n  npm run build:debug\n  npm test\n", o.name, dir)
	return err
}

// checkEmptyDir fails if dir exists and is not empty.
func checkEmptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("%s is not empty", dir)
	}
	return nil
}

// writeProject writes the files of the project: go.mod, package.json and the templates.
func writeProject(dir, binary string, o newOptions) error {
	data := templateData{Name: o.name, Binary: binary, NpmDir: "npm", GoVersion: goVersion, Glibc: zigGlibc}
	for _, t := range o.targets {
		data.Targets = append(data.Targets, templateTarget{t, ciRunners[t]})
	}
	files := map[string][]byte{
		"go.mod": []byte("module " + o.module + "\n\ngo " + goVersion + "\n"),
	}
	pkg, err := newPackageJSON(o, binary)
	if err != nil {
		return err
	}
	if files[packageJSONFile], err = pkg.format(); err != nil {
		return err
	}
	for _, tf := range templateFiles {
		src, err := templates.ReadFile("template/" + tf.tmpl)
		if err != nil {
			return err
		}
		tmpl, err := template.New(tf.tmpl).Delims("[[", "]]").Option("missingkey=error").Parse(string(src))
		if err != nil {
			return err
		}
		var b bytes.Buffer
		if err := tmpl.Execute(&b, data); err != nil {
			return err
		}
		files[tf.file] = b.Bytes()
	}
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// newPackageJSON returns the package.json of a new project.
func newPackageJSON(o newOptions, binary string) (*jsonObject, error) {
	pkg := newJSONObject()
	cfg := newJSONObject()
	scripts := newJSONObject()
	fields := []struct {
		obj *jsonObject
		key string
		val any
	}{
		{scripts, "build", "go tool napi-go build -release"},
		{scripts, "build:debug", "go tool napi-go build"},
		{scripts, "test", "node --test"},
		{cfg, "binaryName", binary},
		{cfg, "targets", o.targets},
		{pkg, "name", o.name},
		{pkg, "version", "0.1.0"},
		{pkg, "description", "A Node.js addon written in Go"},
		{pkg, "main", "index.js"},
		{pkg, "types", "index.d.ts"},
		{pkg, "files", []string{"index.js", "index.d.ts"}},
		{pkg, "engines", map[string]string{"node": ">= 22"}},
		{pkg, "scripts", scripts},
		{pkg, configKey, cfg},
	}
	if strings.HasPrefix(o.name, "@") {
		fields = append(fields, struct {
			obj *jsonObject
			key string
			val any
		}{pkg, "publishConfig", map[string]string{"access": "public"}})
	}
	for _, f := range fields {
		if err := f.obj.set(f.key, f.val); err != nil {
			return nil, err
		}
	}
	return pkg, nil
}

// installProject requires napi-go (as a library and as the napi-go tool), generates the
// registrations and the entry point, and tidies go.mod.
func installProject(dir string, o newOptions, stdout io.Writer) error {
	tool := modulePath + "/cmd/napi-go"
	var steps [][]string
	if o.replace != "" {
		// Any version: the replace directive points to the local copy.
		const localVersion = "v0.0.0-00010101000000-000000000000"
		steps = append(steps, []string{"mod", "edit", "-require=" + modulePath + "@" + localVersion, "-replace=" + modulePath + "=" + o.replace})
	} else {
		steps = append(steps, []string{"get", modulePath + "@" + o.version})
	}
	steps = append(steps, []string{"mod", "edit", "-tool=" + tool})
	for _, args := range steps {
		if err := goCommand(dir, args...); err != nil {
			return err
		}
	}
	p, err := loadProject(dir)
	if err != nil {
		return err
	}
	if err := generate(p.codegenConfig(), false, stdout); err != nil {
		return fmt.Errorf("gen: %w", err)
	}
	return goCommand(dir, "mod", "tidy")
}

// goCommand runs the go command in dir.
func goCommand(dir string, args ...string) error {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
