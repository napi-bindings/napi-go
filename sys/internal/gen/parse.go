package main

import (
	"bufio"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Param è un parametro di una funzione Node-API così come appare nell'header.
type Param struct {
	CType string // tipo normalizzato, es. "const napi_value*"
	Name  string // nome C, es. "argv"
}

// Func è una funzione Node-API dichiarata negli header.
type Func struct {
	Name     string // es. "napi_create_int32"
	Ret      string // "napi_status" oppure "void"
	NoReturn bool   // NAPI_NO_RETURN (napi_fatal_error)
	Params   []Param
	Header   string // file di origine
	Version  int    // versione Node-API minima, da symbols.js
}

// EnumMember è un valore di un enum C con il valore già calcolato.
type EnumMember struct {
	CName string
	Value int64
}

// Enum è un typedef enum degli header.
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

// stripComments rimuove commenti // e /* */. Gli header Node-API non hanno letterali stringa
// contenenti sequenze di commento, quindi non serve gestirli.
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

// stripPreprocessor rimuove le direttive (#if, #define, ...) comprese le righe di continuazione.
// Le dichiarazioni condizionate da NAPI_VERSION vengono quindi tutte incluse: la versione di
// ogni funzione arriva da symbols.js, non dalle #if.
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

// normType normalizza gli spazi di un tipo C: "const char *" → "const char*".
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
			return nil, fmt.Errorf("parametro non riconosciuto: %q", raw)
		}
		params = append(params, Param{CType: normType(m[1]), Name: m[2]})
	}
	return params, nil
}

// parseFuncs estrae le dichiarazioni NAPI_EXTERN da un header.
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
	// Ogni NAPI_EXTERN deve essere stato riconosciuto: una dichiarazione con forma nuova non deve
	// sparire in silenzio dal binding.
	if want := strings.Count(text, "NAPI_EXTERN"); want != len(funcs) {
		return nil, fmt.Errorf("%s: %d dichiarazioni NAPI_EXTERN, %d riconosciute", header, want, len(funcs))
	}
	return funcs, nil
}

// parseEnums estrae i typedef enum e ne calcola i valori.
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

// evalExpr valuta le espressioni costanti usate negli enum Node-API: interi, identificatori già
// definiti, parentesi, << e |.
func evalExpr(expr string, env map[string]int64) (int64, error) {
	p := &exprParser{toks: tokenize(expr), env: env}
	v, err := p.or()
	if err != nil {
		return 0, err
	}
	if p.pos != len(p.toks) {
		return 0, fmt.Errorf("token in eccesso in %q", expr)
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
				toks = append(toks, string(c)) // carattere non previsto: errore nel parser
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
		return 0, fmt.Errorf("espressione incompleta")
	case t == "(":
		v, err := p.or()
		if err != nil {
			return 0, err
		}
		if p.peek() != ")" {
			return 0, fmt.Errorf("manca ')'")
		}
		p.pos++
		return v, nil
	case t[0] >= '0' && t[0] <= '9':
		return strconv.ParseInt(t, 0, 64)
	default:
		v, ok := p.env[t]
		if !ok {
			return 0, fmt.Errorf("identificatore sconosciuto %q", t)
		}
		return v, nil
	}
}

var (
	reVersionBlock = regexp.MustCompile(`^const v(\d+) = \{`)
	reSymbol       = regexp.MustCompile(`'(\w+)'`)
)

// parseSymbols legge symbols.js e restituisce, per ogni funzione, la prima versione Node-API in
// cui compare. Il file elenca le versioni in ordine crescente, ciascuna come superset della
// precedente (spread di vN-1).
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
				return nil, 0, fmt.Errorf("symbols.js: versione %d dopo la %d", v, cur)
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
		return nil, 0, fmt.Errorf("symbols.js: nessun simbolo trovato")
	}
	return versions, maxVersion, nil
}

// assignVersions completa le Func con la versione minima e verifica che header e symbols.js
// descrivano lo stesso insieme di funzioni.
func assignVersions(funcs []Func, versions map[string]int) error {
	declared := map[string]bool{}
	var problems []string
	for i := range funcs {
		v, ok := versions[funcs[i].Name]
		if !ok {
			problems = append(problems, funcs[i].Name+": dichiarata negli header ma assente da symbols.js")
			continue
		}
		funcs[i].Version = v
		declared[funcs[i].Name] = true
	}
	for name := range versions {
		if !declared[name] {
			problems = append(problems, name+": in symbols.js ma non dichiarata negli header")
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("header e symbols.js non coincidono:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}
