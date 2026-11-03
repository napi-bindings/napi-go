package main

import (
	"bufio"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Param is a parameter of a Node-API function as declared in the C header.
type Param struct {
	CType string // normalized C type, e.g. "const napi_value*"
	Name  string // C parameter name, e.g. "argv"
}

// Func is a Node-API function declared in the C headers.
type Func struct {
	Name     string // e.g. "napi_create_int32"
	Ret      string // "napi_status" or "void"
	NoReturn bool   // NAPI_NO_RETURN (napi_fatal_error)
	Params   []Param
	Header   string // source header file
	Version  int    // minimum Node-API version, from symbols.js
}

// EnumMember is a C enum constant with its evaluated integer value.
type EnumMember struct {
	CName string
	Value int64
}

// Enum is a typedef enum parsed from the C headers.
type Enum struct {
	CName   string
	Members []EnumMember
}

var (
	reFunc = regexp.MustCompile(`NAPI_EXTERN\s+(NAPI_NO_RETURN\s+)?(napi_status|void)\s+NAPI_CDECL\s+(\w+)\s*\(([^)]*)\)\s*;`)
	reEnum = regexp.MustCompile(`typedef\s+enum\s*\{([^}]*)\}\s*(\w+)\s*;`)
	reName = regexp.MustCompile(`^(.*?)\s*\b(\w+)$`)
	reStar = regexp.MustCompile(`\s*\*\s*`)
	reWS   = regexp.MustCompile(`\s+`)
)

// stripComments removes // and /* */ comments. The Node-API headers do not contain string literals
// with comment delimiters, so string literal handling is unnecessary.
func stripComments(src string) string {
	var b strings.Builder
	for i := 0; i < len(src); {
		switch {
		case strings.HasPrefix(src[i:], "//"):
			end := strings.IndexByte(src[i:], '\n')
			if end < 0 {
				return b.String()
			}
			i += end
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return b.String()
			}
			i += end + 4
			b.WriteByte(' ')
		default:
			b.WriteByte(src[i])
			i++
		}
	}
	return b.String()
}

// stripPreprocessor removes preprocessor directives (#if, #define, ...) including backslash
// continuation lines. Declarations guarded by NAPI_VERSION are therefore all included; each
// function's minimum version is taken from symbols.js rather than #if guards.
func stripPreprocessor(src string) string {
	var out []string
	cont := false
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if cont || strings.HasPrefix(t, "#") {
			cont = strings.HasSuffix(t, `\`)
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func clean(src string) string {
	return stripPreprocessor(stripComments(src))
}

// normType normalizes whitespace in a C type: "const char *" → "const char*".
func normType(t string) string {
	t = reWS.ReplaceAllString(strings.TrimSpace(t), " ")
	t = reStar.ReplaceAllString(t, "*")
	return t
}

func parseParams(list string) ([]Param, error) {
	list = strings.TrimSpace(reWS.ReplaceAllString(list, " "))
	if list == "" || list == "void" {
		return nil, nil
	}
	var params []Param
	for _, raw := range strings.Split(list, ",") {
		m := reName.FindStringSubmatch(strings.TrimSpace(raw))
		if m == nil || strings.TrimSpace(m[1]) == "" {
			return nil, fmt.Errorf("unrecognized parameter: %q", raw)
		}
		params = append(params, Param{CType: normType(m[1]), Name: m[2]})
	}
	return params, nil
}

// parseFuncs extracts NAPI_EXTERN function declarations from a header file.
func parseFuncs(header, src string) ([]Func, error) {
	text := clean(src)
	var funcs []Func
	for _, m := range reFunc.FindAllStringSubmatch(text, -1) {
		params, err := parseParams(m[4])
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", header, m[3], err)
		}
		funcs = append(funcs, Func{
			Name:     m[3],
			Ret:      m[2],
			NoReturn: m[1] != "",
			Params:   params,
			Header:   header,
		})
	}
	// Every NAPI_EXTERN occurrence must be matched: a declaration with a new shape must not be
	// silently dropped from the bindings.
	if want := strings.Count(text, "NAPI_EXTERN"); want != len(funcs) {
		return nil, fmt.Errorf("%s: %d NAPI_EXTERN declarations, %d matched", header, want, len(funcs))
	}
	return funcs, nil
}

// parseEnums extracts typedef enum blocks and evaluates their member values.
func parseEnums(src string) ([]Enum, error) {
	text := clean(src)
	var enums []Enum
	for _, m := range reEnum.FindAllStringSubmatch(text, -1) {
		e := Enum{CName: m[2]}
		env := map[string]int64{}
		next := int64(0)
		for _, raw := range strings.Split(m[1], ",") {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			name, expr, hasExpr := strings.Cut(raw, "=")
			name = strings.TrimSpace(name)
			v := next
			if hasExpr {
				val, err := evalExpr(strings.TrimSpace(expr), env)
				if err != nil {
					return nil, fmt.Errorf("enum %s, %s: %w", e.CName, name, err)
				}
				v = val
			}
			env[name] = v
			e.Members = append(e.Members, EnumMember{CName: name, Value: v})
			next = v + 1
		}
		enums = append(enums, e)
	}
	return enums, nil
}

// evalExpr evaluates constant expressions used in Node-API enums: integer literals, previously
// defined enum identifiers, parentheses, <<, and |.
func evalExpr(expr string, env map[string]int64) (int64, error) {
	p := &exprParser{toks: tokenize(expr), env: env}
	v, err := p.or()
	if err != nil {
		return 0, err
	}
	if p.pos != len(p.toks) {
		return 0, fmt.Errorf("unexpected trailing tokens in %q", expr)
	}
	return v, nil
}

func tokenize(s string) []string {
	var toks []string
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n':
			i++
		case strings.HasPrefix(s[i:], "<<"):
			toks = append(toks, "<<")
			i += 2
		case c == '|' || c == '(' || c == ')':
			toks = append(toks, string(c))
			i++
		default:
			j := i
			for j < len(s) && (s[j] == '_' || s[j] >= '0' && s[j] <= '9' || s[j] >= 'a' && s[j] <= 'z' || s[j] >= 'A' && s[j] <= 'Z') {
				j++
			}
			if j == i {
				toks = append(toks, string(c)) // unexpected character: will surface as a parser error
				i++
				continue
			}
			toks = append(toks, s[i:j])
			i = j
		}
	}
	return toks
}

type exprParser struct {
	toks []string
	pos  int
	env  map[string]int64
}

func (p *exprParser) peek() string {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return ""
}

func (p *exprParser) or() (int64, error) {
	v, err := p.shift()
	for err == nil && p.peek() == "|" {
		p.pos++
		var r int64
		r, err = p.shift()
		v |= r
	}
	return v, err
}

func (p *exprParser) shift() (int64, error) {
	v, err := p.primary()
	for err == nil && p.peek() == "<<" {
		p.pos++
		var r int64
		r, err = p.primary()
		v <<= r
	}
	return v, err
}

func (p *exprParser) primary() (int64, error) {
	t := p.peek()
	p.pos++
	switch {
	case t == "":
		return 0, fmt.Errorf("incomplete expression")
	case t == "(":
		v, err := p.or()
		if err != nil {
			return 0, err
		}
		if p.peek() != ")" {
			return 0, fmt.Errorf("missing ')'")
		}
		p.pos++
		return v, nil
	case t[0] >= '0' && t[0] <= '9':
		return strconv.ParseInt(t, 0, 64)
	default:
		v, ok := p.env[t]
		if !ok {
			return 0, fmt.Errorf("unknown identifier %q", t)
		}
		return v, nil
	}
}

var (
	reVersionBlock = regexp.MustCompile(`^const v(\d+) = \{`)
	reSymbol       = regexp.MustCompile(`'(\w+)'`)
)

// parseSymbols reads symbols.js and returns the first Node-API version in which each function
// appears. The file lists versions in ascending order, each spreading the previous version's
// arrays (...vN-1).
func parseSymbols(src string) (map[string]int, int, error) {
	versions := map[string]int{}
	cur, maxVersion := 0, 0
	sc := bufio.NewScanner(strings.NewReader(src))
	for sc.Scan() {
		line := sc.Text()
		if m := reVersionBlock.FindStringSubmatch(line); m != nil {
			v, err := strconv.Atoi(m[1])
			if err != nil {
				return nil, 0, err
			}
			if v <= cur {
				return nil, 0, fmt.Errorf("symbols.js: version %d after %d", v, cur)
			}
			cur, maxVersion = v, v
			continue
		}
		if strings.HasPrefix(line, "module.exports") {
			cur = 0
			continue
		}
		if cur == 0 {
			continue
		}
		for _, m := range reSymbol.FindAllStringSubmatch(line, -1) {
			if _, seen := versions[m[1]]; !seen {
				versions[m[1]] = cur
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, 0, err
	}
	if len(versions) == 0 {
		return nil, 0, fmt.Errorf("symbols.js: no symbols found")
	}
	return versions, maxVersion, nil
}

// assignVersions populates each Func's minimum version and verifies that the headers and
// symbols.js describe the exact same set of functions.
func assignVersions(funcs []Func, versions map[string]int) error {
	declared := map[string]bool{}
	var problems []string
	for i := range funcs {
		v, ok := versions[funcs[i].Name]
		if !ok {
			problems = append(problems, funcs[i].Name+": declared in headers but missing from symbols.js")
			continue
		}
		funcs[i].Version = v
		declared[funcs[i].Name] = true
	}
	for name := range versions {
		if !declared[name] {
			problems = append(problems, name+": in symbols.js but not declared in headers")
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("headers and symbols.js do not match:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}
