package server

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The formula language: an expression is parsed into a tree once and the tree
// is walked per row. Nothing is ever executed as code; every operator and every
// function is a Go function in this file, and evaluation is bounded by a step
// budget, a nesting depth and a maximum text length.
//
// Values are nil (empty), float64, string, bool, formulaDate and []any.

const (
	formulaMaxLen   = 10000
	formulaMaxDepth = 100
	formulaMaxSteps = 200000
	formulaMaxText  = 100000
)

var (
	errFormulaEnd      = errors.New("unexpected end")
	errFormulaDivZero  = errors.New("division by zero")
	errFormulaCycle    = errors.New("circular reference in formula")
	errFormulaTooLarge = errors.New("formula does too much work")
)

// formulaDate is a date or a date with a time of day.
type formulaDate struct {
	t       time.Time
	hasTime bool
}

const (
	dayLayout       = "2006-01-02"
	dayTimeLayout   = "2006-01-02T15:04"
	dayTimeTextForm = "2006-01-02 15:04"
)

var formulaDateLayouts = []struct {
	layout  string
	hasTime bool
}{
	{dayLayout, false},
	{dayTimeLayout, true},
	{"2006-01-02T15:04:05", true},
	{dayTimeTextForm, true},
	{time.RFC3339, true},
}

func parseFormulaDate(s string) (formulaDate, bool) {
	s = strings.TrimSpace(s)
	for _, l := range formulaDateLayouts {
		if t, err := time.ParseInLocation(l.layout, s, time.Local); err == nil {
			return formulaDate{t: t, hasTime: l.hasTime}, true
		}
	}
	return formulaDate{}, false
}

// json is how a date travels as a cell value: the same ISO form a date
// property stores.
func (d formulaDate) json() string {
	if d.hasTime {
		return d.t.Format(dayTimeLayout)
	}
	return d.t.Format(dayLayout)
}

// ---- tree ----

type fkind int

const (
	fkNum fkind = iota
	fkStr
	fkBool
	fkList
	fkLegacyRef // {id}: read as a number, the original formula syntax
	fkIdent     // a variable from let/lets, or current/index inside a list function
	fkCall
	fkUnary
	fkBinary
	fkCond
)

type fnode struct {
	kind fkind
	op   string
	num  float64
	str  string
	args []*fnode
}

// ---- lexer ----

type ftoken struct {
	kind string // num, str, ident, ref, op, end
	text string
	pos  int
}

var formulaOps = []string{"==", "!=", "<=", ">=", "&&", "||", "+", "-", "*", "/", "%", "^", "<", ">", "=", "!", "(", ")", "[", "]", ",", ".", "?", ":"}

func lexFormula(src string) ([]ftoken, error) {
	var out []ftoken
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c >= '0' && c <= '9' || c == '.' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9':
			start := i
			for i < len(src) && (src[i] >= '0' && src[i] <= '9' || src[i] == '.') {
				i++
			}
			if i < len(src) && (src[i] == 'e' || src[i] == 'E') {
				j := i + 1
				if j < len(src) && (src[j] == '+' || src[j] == '-') {
					j++
				}
				if j < len(src) && src[j] >= '0' && src[j] <= '9' {
					for j < len(src) && src[j] >= '0' && src[j] <= '9' {
						j++
					}
					i = j
				}
			}
			out = append(out, ftoken{"num", src[start:i], start})
		case c == '"' || c == '\'':
			start := i
			i++
			var b strings.Builder
			closed := false
			for i < len(src) {
				ch := src[i]
				if ch == '\\' && i+1 < len(src) {
					switch src[i+1] {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					default:
						b.WriteByte(src[i+1])
					}
					i += 2
					continue
				}
				if ch == c {
					closed = true
					i++
					break
				}
				b.WriteByte(ch)
				i++
			}
			if !closed {
				return nil, fmt.Errorf("missing closing %c", c)
			}
			out = append(out, ftoken{"str", b.String(), start})
		case c == '{':
			end := strings.IndexByte(src[i:], '}')
			if end < 0 {
				return nil, errors.New("missing }")
			}
			out = append(out, ftoken{"ref", src[i+1 : i+end], i})
			i += end + 1
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			start := i
			for i < len(src) && (src[i] == '_' || src[i] >= 'a' && src[i] <= 'z' || src[i] >= 'A' && src[i] <= 'Z' || src[i] >= '0' && src[i] <= '9') {
				i++
			}
			out = append(out, ftoken{"ident", src[start:i], start})
		default:
			matched := false
			for _, op := range formulaOps {
				if strings.HasPrefix(src[i:], op) {
					out = append(out, ftoken{"op", op, i})
					i += len(op)
					matched = true
					break
				}
			}
			if !matched {
				return nil, fmt.Errorf("unexpected %q", src[i:])
			}
		}
	}
	return append(out, ftoken{"end", "", len(src)}), nil
}

// ---- parser ----

type fparser struct {
	src   string
	toks  []ftoken
	pos   int
	depth int
}

// compileFormula parses an expression into its tree.
func compileFormula(src string) (*fnode, error) {
	if len(src) > formulaMaxLen {
		return nil, fmt.Errorf("formula is longer than %d characters", formulaMaxLen)
	}
	toks, err := lexFormula(src)
	if err != nil {
		return nil, err
	}
	p := &fparser{src: src, toks: toks}
	n, err := p.expr()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != "end" {
		return nil, fmt.Errorf("unexpected %q", src[t.pos:])
	}
	return n, nil
}

func (p *fparser) peek() ftoken { return p.toks[p.pos] }

func (p *fparser) isOp(ops ...string) bool {
	t := p.peek()
	if t.kind == "op" {
		for _, o := range ops {
			if t.text == o {
				return true
			}
		}
	}
	return false
}

func (p *fparser) isWord(words ...string) bool {
	t := p.peek()
	if t.kind == "ident" {
		for _, w := range words {
			if t.text == w {
				return true
			}
		}
	}
	return false
}

func (p *fparser) unexpected() error {
	t := p.peek()
	if t.kind == "end" {
		return errFormulaEnd
	}
	return fmt.Errorf("unexpected %q", p.src[t.pos:])
}

func (p *fparser) expect(op string) error {
	if !p.isOp(op) {
		if p.peek().kind == "end" {
			return fmt.Errorf("missing %s", op)
		}
		return p.unexpected()
	}
	p.pos++
	return nil
}

func (p *fparser) expr() (*fnode, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > formulaMaxDepth {
		return nil, errors.New("formula is nested too deeply")
	}
	return p.ternary()
}

func (p *fparser) ternary() (*fnode, error) {
	c, err := p.or()
	if err != nil || !p.isOp("?") {
		return c, err
	}
	p.pos++
	a, err := p.expr()
	if err != nil {
		return nil, err
	}
	if err := p.expect(":"); err != nil {
		return nil, err
	}
	b, err := p.expr()
	if err != nil {
		return nil, err
	}
	return &fnode{kind: fkCond, args: []*fnode{c, a, b}}, nil
}

func (p *fparser) binaryLevel(next func() (*fnode, error), match func() (string, bool)) (*fnode, error) {
	l, err := next()
	if err != nil {
		return nil, err
	}
	for {
		op, ok := match()
		if !ok {
			return l, nil
		}
		p.pos++
		r, err := next()
		if err != nil {
			return nil, err
		}
		l = &fnode{kind: fkBinary, op: op, args: []*fnode{l, r}}
	}
}

func (p *fparser) or() (*fnode, error) {
	return p.binaryLevel(p.and, func() (string, bool) {
		return "or", p.isOp("||") || p.isWord("or")
	})
}

func (p *fparser) and() (*fnode, error) {
	return p.binaryLevel(p.not, func() (string, bool) {
		return "and", p.isOp("&&") || p.isWord("and")
	})
}

func (p *fparser) not() (*fnode, error) {
	if p.isOp("!") || p.isWord("not") {
		p.pos++
		p.depth++
		defer func() { p.depth-- }()
		if p.depth > formulaMaxDepth {
			return nil, errors.New("formula is nested too deeply")
		}
		v, err := p.not()
		if err != nil {
			return nil, err
		}
		return &fnode{kind: fkUnary, op: "not", args: []*fnode{v}}, nil
	}
	return p.comparison()
}

func (p *fparser) comparison() (*fnode, error) {
	return p.binaryLevel(p.additive, func() (string, bool) {
		if p.isOp("==", "=") {
			return "==", true
		}
		t := p.peek()
		return t.text, p.isOp("!=", "<", "<=", ">", ">=")
	})
}

func (p *fparser) additive() (*fnode, error) {
	return p.binaryLevel(p.multiplicative, func() (string, bool) {
		return p.peek().text, p.isOp("+", "-")
	})
}

func (p *fparser) multiplicative() (*fnode, error) {
	return p.binaryLevel(p.unary, func() (string, bool) {
		return p.peek().text, p.isOp("*", "/", "%")
	})
}

func (p *fparser) unary() (*fnode, error) {
	if p.isOp("-", "+") {
		op := p.peek().text
		p.pos++
		p.depth++
		defer func() { p.depth-- }()
		if p.depth > formulaMaxDepth {
			return nil, errors.New("formula is nested too deeply")
		}
		v, err := p.unary()
		if err != nil {
			return nil, err
		}
		if op == "+" {
			return &fnode{kind: fkUnary, op: "+", args: []*fnode{v}}, nil
		}
		return &fnode{kind: fkUnary, op: "-", args: []*fnode{v}}, nil
	}
	return p.power()
}

func (p *fparser) power() (*fnode, error) {
	base, err := p.postfix()
	if err != nil || !p.isOp("^") {
		return base, err
	}
	p.pos++
	exp, err := p.unary()
	if err != nil {
		return nil, err
	}
	return &fnode{kind: fkBinary, op: "^", args: []*fnode{base, exp}}, nil
}

// postfix reads method calls: x.f(a) is f(x, a).
func (p *fparser) postfix() (*fnode, error) {
	n, err := p.primary()
	if err != nil {
		return nil, err
	}
	for p.isOp(".") {
		p.pos++
		t := p.peek()
		if t.kind != "ident" {
			return nil, p.unexpected()
		}
		p.pos++
		args := []*fnode{n}
		if p.isOp("(") {
			rest, err := p.argList()
			if err != nil {
				return nil, err
			}
			args = append(args, rest...)
		}
		n = &fnode{kind: fkCall, str: t.text, args: args}
	}
	return n, nil
}

func (p *fparser) argList() ([]*fnode, error) {
	if err := p.expect("("); err != nil {
		return nil, err
	}
	var args []*fnode
	if p.isOp(")") {
		p.pos++
		return args, nil
	}
	for {
		a, err := p.expr()
		if err != nil {
			return nil, err
		}
		args = append(args, a)
		if p.isOp(",") {
			p.pos++
			continue
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		return args, nil
	}
}

func (p *fparser) primary() (*fnode, error) {
	t := p.peek()
	switch t.kind {
	case "end":
		return nil, errFormulaEnd
	case "num":
		p.pos++
		f, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", t.text)
		}
		return &fnode{kind: fkNum, num: f}, nil
	case "str":
		p.pos++
		return &fnode{kind: fkStr, str: t.text}, nil
	case "ref":
		p.pos++
		return &fnode{kind: fkLegacyRef, str: t.text}, nil
	case "ident":
		p.pos++
		switch t.text {
		case "true", "false":
			return &fnode{kind: fkBool, num: map[bool]float64{true: 1}[t.text == "true"]}, nil
		}
		if p.isOp("(") {
			args, err := p.argList()
			if err != nil {
				return nil, err
			}
			return &fnode{kind: fkCall, str: t.text, args: args}, nil
		}
		return &fnode{kind: fkIdent, str: t.text}, nil
	}
	if p.isOp("(") {
		p.pos++
		n, err := p.expr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		return n, nil
	}
	if p.isOp("[") {
		p.pos++
		n := &fnode{kind: fkList}
		if p.isOp("]") {
			p.pos++
			return n, nil
		}
		for {
			item, err := p.expr()
			if err != nil {
				return nil, err
			}
			n.args = append(n.args, item)
			if p.isOp(",") {
				p.pos++
				continue
			}
			if err := p.expect("]"); err != nil {
				return nil, err
			}
			return n, nil
		}
	}
	return nil, p.unexpected()
}

// ---- static checks ----

// checkFormula compiles an expression and checks the names in it against the
// schema: unknown functions, unknown properties named with prop("…"), and the
// argument counts of every call. It does not evaluate anything.
func checkFormula(src string, schema []propDef) error {
	n, err := compileFormula(src)
	if err != nil {
		return err
	}
	idx := newPropIndex(schema)
	return checkNode(n, idx, map[string]bool{})
}

func checkNode(n *fnode, idx propIndex, vars map[string]bool) error {
	switch n.kind {
	case fkIdent:
		if !vars[n.str] {
			return fmt.Errorf("unknown name %q", n.str)
		}
		return nil
	case fkCall:
		name := n.str
		if name == "prop" {
			if len(n.args) != 1 {
				return errors.New("prop() takes one property name")
			}
			if n.args[0].kind == fkStr {
				if _, ok := idx.resolve(n.args[0].str); !ok {
					return fmt.Errorf("unknown property %q", n.args[0].str)
				}
			}
			return nil
		}
		fn, ok := formulaFuncs[name]
		if !ok {
			return fmt.Errorf("unknown function %s()", name)
		}
		if len(n.args) < fn.min || fn.max >= 0 && len(n.args) > fn.max {
			return fmt.Errorf("%s() takes %s", name, fn.arity())
		}
		inner := vars
		switch name {
		case "let", "lets":
			inner = copyVars(vars)
			for i := 0; i+1 < len(n.args); i += 2 {
				if n.args[i].kind != fkIdent {
					return fmt.Errorf("%s() needs a name before each value", name)
				}
				if err := checkNode(n.args[i+1], idx, inner); err != nil {
					return err
				}
				inner[n.args[i].str] = true
			}
			return checkNode(n.args[len(n.args)-1], idx, inner)
		case "map", "filter", "find", "findIndex", "some", "every":
			if err := checkNode(n.args[0], idx, vars); err != nil {
				return err
			}
			inner = copyVars(vars)
			inner["current"], inner["index"] = true, true
			return checkNode(n.args[1], idx, inner)
		}
		for _, a := range n.args {
			if err := checkNode(a, idx, inner); err != nil {
				return err
			}
		}
		return nil
	}
	for _, a := range n.args {
		if err := checkNode(a, idx, vars); err != nil {
			return err
		}
	}
	return nil
}

func copyVars(vars map[string]bool) map[string]bool {
	out := make(map[string]bool, len(vars)+2)
	for k, v := range vars {
		out[k] = v
	}
	return out
}

// ---- property lookup ----

// propIndex finds a property by id, by exact name, or by name ignoring case.
type propIndex struct {
	byID   map[string]propDef
	byName map[string]string
	byFold map[string]string
}

func newPropIndex(schema []propDef) propIndex {
	idx := propIndex{byID: map[string]propDef{}, byName: map[string]string{}, byFold: map[string]string{}}
	for _, d := range schema {
		idx.byID[d.ID] = d
		if _, taken := idx.byName[d.Name]; !taken {
			idx.byName[d.Name] = d.ID
		}
		if _, taken := idx.byFold[strings.ToLower(d.Name)]; !taken {
			idx.byFold[strings.ToLower(d.Name)] = d.ID
		}
	}
	return idx
}

// titleRef is what prop() resolves to when no property has the name: the
// row's title, under the names it goes by.
var titleRefs = map[string]bool{"title": true, "name": true}

const titlePropID = "\x00title"

func (idx propIndex) resolve(ref string) (string, bool) {
	if id, ok := idx.byName[ref]; ok {
		return id, true
	}
	if _, ok := idx.byID[ref]; ok {
		return ref, true
	}
	if id, ok := idx.byFold[strings.ToLower(ref)]; ok {
		return id, true
	}
	if titleRefs[strings.ToLower(ref)] {
		return titlePropID, true
	}
	return "", false
}

// ---- evaluation ----

// formulaRow is one row a formula is evaluated against. props already holds
// backrelations and rollups; formulas are evaluated on demand.
type formulaRow struct {
	id, title string
	props     map[string]any
}

// formulaEnv evaluates the formulas of one row.
type formulaEnv struct {
	row      formulaRow
	idx      propIndex
	compiled map[string]compiledFormula
	visiting map[string]bool
	done     map[string]formulaResult
	steps    int
	now      time.Time
}

type compiledFormula struct {
	node *fnode
	err  error
}

type formulaResult struct {
	val any
	err error
}

type fscope struct {
	name   string
	val    any
	parent *fscope
}

func (sc *fscope) lookup(name string) (any, bool) {
	for s := sc; s != nil; s = s.parent {
		if s.name == name {
			return s.val, true
		}
	}
	return nil, false
}

// compileFormulas compiles every formula of a schema once, for all rows.
func compileFormulas(schema []propDef) map[string]compiledFormula {
	out := map[string]compiledFormula{}
	for _, d := range schema {
		if d.Type == "formula" {
			n, err := compileFormula(d.Formula)
			out[d.ID] = compiledFormula{node: n, err: err}
		}
	}
	return out
}

func newFormulaEnv(row formulaRow, idx propIndex, compiled map[string]compiledFormula, now time.Time) *formulaEnv {
	return &formulaEnv{row: row, idx: idx, compiled: compiled, visiting: map[string]bool{}, done: map[string]formulaResult{}, now: now}
}

// formula evaluates one formula property of the row, at most once.
func (e *formulaEnv) formula(id string) (any, error) {
	if r, ok := e.done[id]; ok {
		return r.val, r.err
	}
	c := e.compiled[id]
	if c.err != nil {
		return nil, c.err
	}
	if e.visiting[id] {
		return nil, errFormulaCycle
	}
	e.visiting[id] = true
	v, err := e.eval(c.node, nil)
	delete(e.visiting, id)
	if err == nil {
		v, err = finishValue(v)
	}
	// A cycle is not cached: the formula that closed the loop reports it, and
	// every formula on the way passes it outward.
	e.done[id] = formulaResult{v, err}
	return v, err
}

// finishValue rejects numbers that cannot be shown.
func finishValue(v any) (any, error) {
	if f, ok := v.(float64); ok && (math.IsNaN(f) || math.IsInf(f, 0)) {
		return nil, errors.New("result is not a number")
	}
	return v, nil
}

// propValue reads a property of the row as a typed formula value.
func (e *formulaEnv) propValue(id string) (any, error) {
	if id == titlePropID {
		return e.row.title, nil
	}
	d := e.idx.byID[id]
	if d.Type == "formula" {
		return e.formula(id)
	}
	return typedPropValue(d, e.row.props[id]), nil
}

// typedPropValue turns a stored or computed cell into a formula value.
func typedPropValue(d propDef, raw any) any {
	if d.Type == "checkbox" {
		return raw == true
	}
	if raw == nil {
		return nil
	}
	optionName := func(id string) string {
		for _, o := range d.Options {
			if o.ID == id {
				return o.Name
			}
		}
		return id
	}
	switch d.Type {
	case "number":
		if n, ok := toNumber(raw); ok {
			return n
		}
		return nil
	case "date":
		if s, ok := raw.(string); ok {
			if s == "" {
				return nil
			}
			if dt, ok := parseFormulaDate(s); ok {
				return dt
			}
			return s
		}
	case "select":
		if s, ok := raw.(string); ok {
			if s == "" {
				return nil
			}
			return optionName(s)
		}
	case "multiselect":
		if l, ok := raw.([]any); ok {
			out := make([]any, 0, len(l))
			for _, x := range l {
				if s, ok := x.(string); ok {
					out = append(out, optionName(s))
				}
			}
			return out
		}
	case "checklist":
		if l, ok := raw.([]any); ok {
			out := make([]any, 0, len(l))
			for _, x := range l {
				if m, ok := x.(map[string]any); ok {
					out = append(out, m["text"])
				}
			}
			return out
		}
	case "lastActivity":
		if m, ok := raw.(map[string]any); ok {
			if at, ok := m["at"].(string); ok {
				if dt, ok := parseFormulaDate(at); ok {
					return dt
				}
			}
			return nil
		}
	}
	return plainFormulaValue(raw)
}

// plainFormulaValue converts JSON-shaped data into formula values.
func plainFormulaValue(raw any) any {
	switch t := raw.(type) {
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = plainFormulaValue(x)
		}
		return out
	case map[string]any:
		return fmt.Sprint(t)
	}
	return raw
}

// legacyRef is {id}: the original syntax, read as a number. Anything that is
// not one reads as 0, and so does an id that does not exist.
func (e *formulaEnv) legacyRef(id string) (any, error) {
	if _, ok := e.compiled[id]; ok {
		v, err := e.formula(id)
		if err != nil {
			return nil, err
		}
		n, _ := toNumber(v)
		return n, nil
	}
	n, _ := toNumber(e.row.props[id])
	return n, nil
}

func (e *formulaEnv) tick() error {
	e.steps++
	if e.steps > formulaMaxSteps {
		return errFormulaTooLarge
	}
	return nil
}

func (e *formulaEnv) eval(n *fnode, sc *fscope) (any, error) {
	if err := e.tick(); err != nil {
		return nil, err
	}
	switch n.kind {
	case fkNum:
		return n.num, nil
	case fkStr:
		return n.str, nil
	case fkBool:
		return n.num == 1, nil
	case fkList:
		out := make([]any, 0, len(n.args))
		for _, a := range n.args {
			v, err := e.eval(a, sc)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case fkLegacyRef:
		return e.legacyRef(n.str)
	case fkIdent:
		if v, ok := sc.lookup(n.str); ok {
			return v, nil
		}
		return nil, fmt.Errorf("unknown name %q", n.str)
	case fkCond:
		c, err := e.evalBool(n.args[0], sc)
		if err != nil {
			return nil, err
		}
		if c {
			return e.eval(n.args[1], sc)
		}
		return e.eval(n.args[2], sc)
	case fkUnary:
		switch n.op {
		case "not":
			b, err := e.evalBool(n.args[0], sc)
			return !b, err
		case "+":
			return e.evalNum(n.args[0], sc)
		default:
			v, err := e.evalNum(n.args[0], sc)
			return -v, err
		}
	case fkBinary:
		return e.binary(n, sc)
	case fkCall:
		return e.call(n, sc)
	}
	return nil, errors.New("unknown expression")
}

func (e *formulaEnv) evalNum(n *fnode, sc *fscope) (float64, error) {
	v, err := e.eval(n, sc)
	if err != nil {
		return 0, err
	}
	return numberArg(v)
}

func (e *formulaEnv) evalBool(n *fnode, sc *fscope) (bool, error) {
	v, err := e.eval(n, sc)
	if err != nil {
		return false, err
	}
	return formulaTruthy(v), nil
}

func (e *formulaEnv) binary(n *fnode, sc *fscope) (any, error) {
	switch n.op {
	case "and":
		l, err := e.evalBool(n.args[0], sc)
		if err != nil || !l {
			return false, err
		}
		return e.evalBool(n.args[1], sc)
	case "or":
		l, err := e.evalBool(n.args[0], sc)
		if err != nil || l {
			return l, err
		}
		return e.evalBool(n.args[1], sc)
	}
	l, err := e.eval(n.args[0], sc)
	if err != nil {
		return nil, err
	}
	r, err := e.eval(n.args[1], sc)
	if err != nil {
		return nil, err
	}
	switch n.op {
	case "==":
		return formulaEqual(l, r), nil
	case "!=":
		return !formulaEqual(l, r), nil
	case "<", "<=", ">", ">=":
		c, err := formulaCompare(l, r)
		if err != nil {
			return nil, err
		}
		switch n.op {
		case "<":
			return c < 0, nil
		case "<=":
			return c <= 0, nil
		case ">":
			return c > 0, nil
		}
		return c >= 0, nil
	case "+":
		return formulaAdd(l, r)
	}
	a, err := numberArg(l)
	if err != nil {
		return nil, err
	}
	b, err := numberArg(r)
	if err != nil {
		return nil, err
	}
	return arithmetic(n.op, a, b)
}

func arithmetic(op string, a, b float64) (any, error) {
	switch op {
	case "-":
		return a - b, nil
	case "*":
		return a * b, nil
	case "/":
		if b == 0 {
			return nil, errFormulaDivZero
		}
		return a / b, nil
	case "%":
		if b == 0 {
			return nil, errFormulaDivZero
		}
		return math.Mod(a, b), nil
	case "^":
		return math.Pow(a, b), nil
	}
	return nil, fmt.Errorf("unknown operator %s", op)
}

// formulaAdd adds numbers and joins text: as soon as one side is text, the
// other side is written out and appended.
func formulaAdd(l, r any) (any, error) {
	_, ls := l.(string)
	_, rs := r.(string)
	if ls || rs {
		return limitText(formatFormulaValue(l) + formatFormulaValue(r))
	}
	if d, ok := l.(formulaDate); ok {
		return nil, fmt.Errorf("use dateAdd() to add to a date, not + (got %s)", d.json())
	}
	a, err := numberArg(l)
	if err != nil {
		return nil, err
	}
	b, err := numberArg(r)
	if err != nil {
		return nil, err
	}
	return a + b, nil
}

func limitText(s string) (any, error) {
	if len(s) > formulaMaxText {
		return nil, errFormulaTooLarge
	}
	return s, nil
}

// typeName names a value's type in error messages.
func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "empty"
	case float64:
		return "number"
	case string:
		return "text"
	case bool:
		return "checkbox"
	case formulaDate:
		return "date"
	case []any:
		return "list"
	}
	return "value"
}

// numberArg reads a value as a number. Empty is 0 and a checkbox is 1 or 0;
// text, dates and lists are refused rather than read as 0.
func numberArg(v any) (float64, error) {
	switch t := v.(type) {
	case nil:
		return 0, nil
	case float64:
		return t, nil
	case bool:
		if t {
			return 1, nil
		}
		return 0, nil
	}
	return 0, fmt.Errorf("expected a number, got %s", typeName(v))
}

func textArg(v any) string { return formatFormulaValue(v) }

func formulaTruthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case float64:
		return t != 0
	case string:
		return t != ""
	case []any:
		return len(t) > 0
	}
	return true
}

func isEmptyFormulaValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case []any:
		return len(t) == 0
	case bool:
		return !t
	case float64:
		return t == 0
	}
	return false
}

func formulaEqual(l, r any) bool {
	if l == nil || r == nil {
		other := l
		if l == nil {
			other = r
		}
		switch t := other.(type) {
		case nil:
			return true
		case string:
			return t == ""
		case []any:
			return len(t) == 0
		}
		return false
	}
	switch a := l.(type) {
	case formulaDate:
		b, ok := r.(formulaDate)
		return ok && a.t.Equal(b.t)
	case []any:
		b, ok := r.([]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for i := range a {
			if !formulaEqual(a[i], b[i]) {
				return false
			}
		}
		return true
	}
	return l == r
}

func formulaCompare(l, r any) (int, error) {
	switch a := l.(type) {
	case formulaDate:
		if b, ok := r.(formulaDate); ok {
			return a.t.Compare(b.t), nil
		}
	case string:
		if b, ok := r.(string); ok {
			return strings.Compare(a, b), nil
		}
	}
	if typeName(l) == "date" || typeName(r) == "date" || typeName(l) == "text" || typeName(r) == "text" {
		return 0, fmt.Errorf("cannot compare %s with %s", typeName(l), typeName(r))
	}
	a, err := numberArg(l)
	if err != nil {
		return 0, err
	}
	b, err := numberArg(r)
	if err != nil {
		return 0, err
	}
	switch {
	case a < b:
		return -1, nil
	case a > b:
		return 1, nil
	}
	return 0, nil
}

// formatNumberText writes a number without floating-point noise.
func formatNumberText(f float64) string {
	r := math.Round(f*1e10) / 1e10
	if math.Abs(r) >= 1e15 {
		r = f
	}
	return strconv.FormatFloat(r, 'f', -1, 64)
}

// formatFormulaValue is format(): any value written out as text.
func formatFormulaValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case float64:
		return formatNumberText(t)
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case formulaDate:
		if t.hasTime {
			return t.t.Format(dayTimeTextForm)
		}
		return t.t.Format(dayLayout)
	case []any:
		parts := make([]string, len(t))
		for i, x := range t {
			parts[i] = formatFormulaValue(x)
		}
		return strings.Join(parts, ", ")
	}
	return fmt.Sprint(v)
}

// formulaJSON turns a result into the cell value that travels to clients.
func formulaJSON(v any) any {
	switch t := v.(type) {
	case formulaDate:
		return t.json()
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = formulaJSON(x)
		}
		return out
	}
	return v
}

// ---- functions ----

type formulaFunc struct {
	min, max int // max -1: any number
	// lazy functions receive their arguments unevaluated.
	lazy func(e *formulaEnv, args []*fnode, sc *fscope) (any, error)
	fn   func(e *formulaEnv, args []any) (any, error)
}

func (f formulaFunc) arity() string {
	switch {
	case f.max < 0:
		return fmt.Sprintf("at least %d arguments", f.min)
	case f.min == f.max && f.min == 1:
		return "1 argument"
	case f.min == f.max:
		return fmt.Sprintf("%d arguments", f.min)
	}
	return fmt.Sprintf("%d to %d arguments", f.min, f.max)
}

func (e *formulaEnv) call(n *fnode, sc *fscope) (any, error) {
	if n.str == "prop" {
		if len(n.args) != 1 {
			return nil, errors.New("prop() takes one property name")
		}
		ref, err := e.eval(n.args[0], sc)
		if err != nil {
			return nil, err
		}
		id, ok := e.idx.resolve(textArg(ref))
		if !ok {
			return nil, fmt.Errorf("unknown property %q", textArg(ref))
		}
		return e.propValue(id)
	}
	fn, ok := formulaFuncs[n.str]
	if !ok {
		return nil, fmt.Errorf("unknown function %s()", n.str)
	}
	if len(n.args) < fn.min || fn.max >= 0 && len(n.args) > fn.max {
		return nil, fmt.Errorf("%s() takes %s", n.str, fn.arity())
	}
	if fn.lazy != nil {
		return fn.lazy(e, n.args, sc)
	}
	vals := make([]any, len(n.args))
	for i, a := range n.args {
		v, err := e.eval(a, sc)
		if err != nil {
			return nil, err
		}
		vals[i] = v
	}
	v, err := fn.fn(e, vals)
	if err != nil {
		return nil, fmt.Errorf("%s(): %w", n.str, err)
	}
	return v, nil
}

func num1(f func(float64) float64) formulaFunc {
	return formulaFunc{min: 1, max: 1, fn: func(_ *formulaEnv, a []any) (any, error) {
		x, err := numberArg(a[0])
		if err != nil {
			return nil, err
		}
		return f(x), nil
	}}
}

func text1(f func(string) string) formulaFunc {
	return formulaFunc{min: 1, max: 1, fn: func(_ *formulaEnv, a []any) (any, error) {
		return f(textArg(a[0])), nil
	}}
}

func constant(v float64) formulaFunc {
	return formulaFunc{min: 0, max: 0, fn: func(*formulaEnv, []any) (any, error) { return v, nil }}
}

// numbersOf flattens arguments into numbers: min(1, 2) and min([1, 2]) are the
// same. Empty values are skipped.
func numbersOf(args []any) ([]float64, error) {
	var out []float64
	for _, a := range args {
		if l, ok := a.([]any); ok {
			inner, err := numbersOf(l)
			if err != nil {
				return nil, err
			}
			out = append(out, inner...)
			continue
		}
		if a == nil {
			continue
		}
		f, err := numberArg(a)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

func numbersFunc(f func([]float64) any) formulaFunc {
	return formulaFunc{min: 1, max: -1, fn: func(_ *formulaEnv, a []any) (any, error) {
		nums, err := numbersOf(a)
		if err != nil {
			return nil, err
		}
		return f(nums), nil
	}}
}

func listArg(v any) ([]any, error) {
	switch t := v.(type) {
	case []any:
		return t, nil
	case nil:
		return []any{}, nil
	}
	return nil, fmt.Errorf("expected a list, got %s", typeName(v))
}

func dateArg(v any) (formulaDate, error) {
	switch t := v.(type) {
	case formulaDate:
		return t, nil
	case string:
		if d, ok := parseFormulaDate(t); ok {
			return d, nil
		}
	}
	return formulaDate{}, fmt.Errorf("expected a date, got %s", typeName(v))
}

func intArg(v any) (int, error) {
	f, err := numberArg(v)
	return int(f), err
}

func compileRegex(pattern string) (*regexp.Regexp, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern %q", pattern)
	}
	return re, nil
}

// sliceBounds resolves start/end like JavaScript's slice: negative counts from
// the end, and both are clamped to the length.
func sliceBounds(n int, args []any) (int, int, error) {
	start, end := 0, n
	if len(args) > 0 {
		s, err := intArg(args[0])
		if err != nil {
			return 0, 0, err
		}
		start = s
	}
	if len(args) > 1 {
		x, err := intArg(args[1])
		if err != nil {
			return 0, 0, err
		}
		end = x
	}
	clamp := func(i int) int {
		if i < 0 {
			i += n
		}
		return max(0, min(n, i))
	}
	start, end = clamp(start), clamp(end)
	if end < start {
		end = start
	}
	return start, end, nil
}

// dateUnits are the units dateAdd, dateSubtract and dateBetween accept.
var dateUnits = map[string]string{
	"year": "years", "years": "years", "quarter": "quarters", "quarters": "quarters",
	"month": "months", "months": "months", "week": "weeks", "weeks": "weeks",
	"day": "days", "days": "days", "hour": "hours", "hours": "hours",
	"minute": "minutes", "minutes": "minutes",
}

func unitArg(v any) (string, error) {
	u, ok := dateUnits[strings.ToLower(textArg(v))]
	if !ok {
		return "", fmt.Errorf("unknown unit %q: use years, quarters, months, weeks, days, hours or minutes", textArg(v))
	}
	return u, nil
}

func addToDate(d formulaDate, n float64, unit string) formulaDate {
	whole := int(n)
	switch unit {
	case "years":
		d.t = d.t.AddDate(whole, 0, 0)
	case "quarters":
		d.t = d.t.AddDate(0, 3*whole, 0)
	case "months":
		d.t = d.t.AddDate(0, whole, 0)
	case "weeks":
		d.t = d.t.AddDate(0, 0, 7*whole)
	case "days":
		d.t = d.t.AddDate(0, 0, whole)
	case "hours":
		d.t = d.t.Add(time.Duration(n * float64(time.Hour)))
		d.hasTime = true
	case "minutes":
		d.t = d.t.Add(time.Duration(n * float64(time.Minute)))
		d.hasTime = true
	}
	return d
}

// wallClock is a time with its local wall-clock reading moved to UTC, so a
// difference in days does not shift by an hour across a DST change.
func wallClock(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
}

// dateBetween is a minus b in whole units, truncated toward zero.
func dateBetween(a, b time.Time, unit string) float64 {
	a, b = wallClock(a), wallClock(b)
	switch unit {
	case "years", "quarters", "months":
		m := (a.Year()-b.Year())*12 + int(a.Month()) - int(b.Month())
		if m > 0 && b.AddDate(0, m, 0).After(a) {
			m--
		}
		if m < 0 && b.AddDate(0, m, 0).Before(a) {
			m++
		}
		switch unit {
		case "years":
			return float64(m / 12)
		case "quarters":
			return float64(m / 3)
		}
		return float64(m)
	}
	per := map[string]time.Duration{"weeks": 7 * 24 * time.Hour, "days": 24 * time.Hour, "hours": time.Hour, "minutes": time.Minute}[unit]
	return float64(a.Sub(b) / per)
}

var monthNames = []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
var weekdayNames = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

// formatDateTokens are the tokens formatDate() understands, longest first.
var formatDateTokens = []string{"YYYY", "MMMM", "dddd", "MMM", "ddd", "YY", "MM", "DD", "HH", "hh", "mm", "ss", "M", "D", "H", "h", "m", "s", "A", "a"}

// formatDate writes a date with Notion-style tokens (YYYY-MM-DD, MMMM D,
// HH:mm, h:mm A). Text in [brackets] is written as it stands.
func formatDate(t time.Time, layout string) string {
	var b strings.Builder
	for i := 0; i < len(layout); {
		if layout[i] == '[' {
			if end := strings.IndexByte(layout[i:], ']'); end > 0 {
				b.WriteString(layout[i+1 : i+end])
				i += end + 1
				continue
			}
		}
		tok := ""
		for _, k := range formatDateTokens {
			if strings.HasPrefix(layout[i:], k) {
				tok = k
				break
			}
		}
		if tok == "" {
			b.WriteByte(layout[i])
			i++
			continue
		}
		h12 := t.Hour() % 12
		if h12 == 0 {
			h12 = 12
		}
		switch tok {
		case "YYYY":
			b.WriteString(strconv.Itoa(t.Year()))
		case "YY":
			fmt.Fprintf(&b, "%02d", t.Year()%100)
		case "MMMM":
			b.WriteString(monthNames[t.Month()-1])
		case "MMM":
			b.WriteString(monthNames[t.Month()-1][:3])
		case "MM":
			fmt.Fprintf(&b, "%02d", int(t.Month()))
		case "M":
			b.WriteString(strconv.Itoa(int(t.Month())))
		case "dddd":
			b.WriteString(weekdayNames[t.Weekday()])
		case "ddd":
			b.WriteString(weekdayNames[t.Weekday()][:3])
		case "DD":
			fmt.Fprintf(&b, "%02d", t.Day())
		case "D":
			b.WriteString(strconv.Itoa(t.Day()))
		case "HH":
			fmt.Fprintf(&b, "%02d", t.Hour())
		case "H":
			b.WriteString(strconv.Itoa(t.Hour()))
		case "hh":
			fmt.Fprintf(&b, "%02d", h12)
		case "h":
			b.WriteString(strconv.Itoa(h12))
		case "mm":
			fmt.Fprintf(&b, "%02d", t.Minute())
		case "m":
			b.WriteString(strconv.Itoa(t.Minute()))
		case "ss":
			fmt.Fprintf(&b, "%02d", t.Second())
		case "s":
			b.WriteString(strconv.Itoa(t.Second()))
		case "A":
			b.WriteString(map[bool]string{true: "PM", false: "AM"}[t.Hour() >= 12])
		case "a":
			b.WriteString(map[bool]string{true: "pm", false: "am"}[t.Hour() >= 12])
		}
		i += len(tok)
	}
	return b.String()
}

// listFunc runs a list function whose second argument is an expression over
// each item, with `current` and `index` bound.
func listFunc(each func(e *formulaEnv, list []any, test func(int) (any, error)) (any, error)) formulaFunc {
	return formulaFunc{min: 2, max: 2, lazy: func(e *formulaEnv, args []*fnode, sc *fscope) (any, error) {
		v, err := e.eval(args[0], sc)
		if err != nil {
			return nil, err
		}
		list, err := listArg(v)
		if err != nil {
			return nil, err
		}
		test := func(i int) (any, error) {
			inner := &fscope{name: "index", val: float64(i), parent: &fscope{name: "current", val: list[i], parent: sc}}
			return e.eval(args[1], inner)
		}
		return each(e, list, test)
	}}
}

func letFunc(e *formulaEnv, args []*fnode, sc *fscope) (any, error) {
	if len(args)%2 == 0 {
		return nil, errors.New("let() needs a name and a value for each variable, then the expression")
	}
	for i := 0; i+1 < len(args); i += 2 {
		if args[i].kind != fkIdent {
			return nil, errors.New("let() needs a name before each value")
		}
		v, err := e.eval(args[i+1], sc)
		if err != nil {
			return nil, err
		}
		sc = &fscope{name: args[i].str, val: v, parent: sc}
	}
	return e.eval(args[len(args)-1], sc)
}

func sortedCopy(list []any) ([]any, error) {
	out := append([]any(nil), list...)
	var err error
	sort.SliceStable(out, func(i, j int) bool {
		c, cerr := formulaCompare(out[i], out[j])
		if cerr != nil {
			err = cerr
		}
		return c < 0
	})
	return out, err
}

var formulaFuncs map[string]formulaFunc

func init() {
	formulaFuncs = map[string]formulaFunc{
		// logic
		"if": {min: 3, max: 3, lazy: func(e *formulaEnv, a []*fnode, sc *fscope) (any, error) {
			c, err := e.evalBool(a[0], sc)
			if err != nil {
				return nil, err
			}
			if c {
				return e.eval(a[1], sc)
			}
			return e.eval(a[2], sc)
		}},
		"ifs": {min: 2, max: -1, lazy: func(e *formulaEnv, a []*fnode, sc *fscope) (any, error) {
			for i := 0; i+1 < len(a); i += 2 {
				c, err := e.evalBool(a[i], sc)
				if err != nil {
					return nil, err
				}
				if c {
					return e.eval(a[i+1], sc)
				}
			}
			if len(a)%2 == 1 {
				return e.eval(a[len(a)-1], sc)
			}
			return nil, nil
		}},
		"empty": {min: 1, max: 1, fn: func(_ *formulaEnv, a []any) (any, error) { return isEmptyFormulaValue(a[0]), nil }},
		"let":   {min: 3, max: 3, lazy: letFunc},
		"lets":  {min: 3, max: -1, lazy: letFunc},
		// math
		"abs":   num1(math.Abs),
		"floor": num1(math.Floor),
		"ceil":  num1(math.Ceil),
		"sqrt":  num1(math.Sqrt),
		"cbrt":  num1(math.Cbrt),
		"exp":   num1(math.Exp),
		"ln":    num1(math.Log),
		"log10": num1(math.Log10),
		"log2":  num1(math.Log2),
		"sign": num1(func(x float64) float64 {
			switch {
			case x > 0:
				return 1
			case x < 0:
				return -1
			}
			return 0
		}),
		"pi": constant(math.Pi),
		"e":  constant(math.E),
		"round": {min: 1, max: 2, fn: func(_ *formulaEnv, a []any) (any, error) {
			x, err := numberArg(a[0])
			if err != nil {
				return nil, err
			}
			places := 0
			if len(a) > 1 {
				if places, err = intArg(a[1]); err != nil {
					return nil, err
				}
			}
			p := math.Pow(10, float64(places))
			return math.Round(x*p) / p, nil
		}},
		"pow": {min: 2, max: 2, fn: func(_ *formulaEnv, a []any) (any, error) {
			x, err := numberArg(a[0])
			if err != nil {
				return nil, err
			}
			y, err := numberArg(a[1])
			if err != nil {
				return nil, err
			}
			return math.Pow(x, y), nil
		}},
		"mod": {min: 2, max: 2, fn: func(_ *formulaEnv, a []any) (any, error) {
			x, err := numberArg(a[0])
			if err != nil {
				return nil, err
			}
			y, err := numberArg(a[1])
			if err != nil {
				return nil, err
			}
			return arithmetic("%", x, y)
		}},
		"sum":    numbersFunc(func(n []float64) any { return calcSumOf(n) }),
		"mean":   numbersFunc(func(n []float64) any { return calcMeanOf(n) }),
		"median": numbersFunc(func(n []float64) any { return calcMedianOf(n) }),
		"min":    numbersFunc(func(n []float64) any { return calcMinOf(n) }),
		"max":    numbersFunc(func(n []float64) any { return calcMaxOf(n) }),
		"toNumber": {min: 1, max: 1, fn: func(_ *formulaEnv, a []any) (any, error) {
			switch t := a[0].(type) {
			case formulaDate:
				return float64(t.t.UnixMilli()), nil
			case bool, float64, nil:
				return numberArg(t)
			}
			if f, err := strconv.ParseFloat(strings.TrimSpace(textArg(a[0])), 64); err == nil {
				return f, nil
			}
			return nil, nil
		}},
		// text
		"format": {min: 1, max: 1, fn: func(_ *formulaEnv, a []any) (any, error) { return formatFormulaValue(a[0]), nil }},
		"length": {min: 1, max: 1, fn: func(_ *formulaEnv, a []any) (any, error) {
			if l, ok := a[0].([]any); ok {
				return float64(len(l)), nil
			}
			return float64(len([]rune(textArg(a[0])))), nil
		}},
		"lower": text1(strings.ToLower),
		"upper": text1(strings.ToUpper),
		"trim":  text1(strings.TrimSpace),
		"concat": {min: 1, max: -1, fn: func(_ *formulaEnv, a []any) (any, error) {
			allLists := true
			for _, x := range a {
				if _, ok := x.([]any); !ok {
					allLists = false
				}
			}
			if allLists {
				var out []any
				for _, x := range a {
					out = append(out, x.([]any)...)
				}
				return out, nil
			}
			var b strings.Builder
			for _, x := range a {
				b.WriteString(textArg(x))
			}
			return limitText(b.String())
		}},
		"join": {min: 1, max: 2, fn: func(_ *formulaEnv, a []any) (any, error) {
			list, err := listArg(a[0])
			if err != nil {
				return nil, err
			}
			sep := ""
			if len(a) > 1 {
				sep = textArg(a[1])
			}
			parts := make([]string, len(list))
			for i, x := range list {
				parts[i] = textArg(x)
			}
			return limitText(strings.Join(parts, sep))
		}},
		"split": {min: 1, max: 2, fn: func(_ *formulaEnv, a []any) (any, error) {
			sep := ""
			if len(a) > 1 {
				sep = textArg(a[1])
			}
			parts := strings.Split(textArg(a[0]), sep)
			out := make([]any, len(parts))
			for i, p := range parts {
				out[i] = p
			}
			return out, nil
		}},
		"contains": {min: 2, max: 2, fn: func(_ *formulaEnv, a []any) (any, error) {
			if l, ok := a[0].([]any); ok {
				for _, x := range l {
					if formulaEqual(x, a[1]) || textArg(x) == textArg(a[1]) {
						return true, nil
					}
				}
				return false, nil
			}
			return strings.Contains(textArg(a[0]), textArg(a[1])), nil
		}},
		"startsWith": {min: 2, max: 2, fn: func(_ *formulaEnv, a []any) (any, error) {
			return strings.HasPrefix(textArg(a[0]), textArg(a[1])), nil
		}},
		"endsWith": {min: 2, max: 2, fn: func(_ *formulaEnv, a []any) (any, error) {
			return strings.HasSuffix(textArg(a[0]), textArg(a[1])), nil
		}},
		"test": {min: 2, max: 2, fn: func(_ *formulaEnv, a []any) (any, error) {
			re, err := compileRegex(textArg(a[1]))
			if err != nil {
				return nil, err
			}
			return re.MatchString(textArg(a[0])), nil
		}},
		"match": {min: 2, max: 2, fn: func(_ *formulaEnv, a []any) (any, error) {
			re, err := compileRegex(textArg(a[1]))
			if err != nil {
				return nil, err
			}
			found := re.FindAllString(textArg(a[0]), 1000)
			out := make([]any, len(found))
			for i, f := range found {
				out[i] = f
			}
			return out, nil
		}},
		"replace": {min: 3, max: 3, fn: func(_ *formulaEnv, a []any) (any, error) {
			re, err := compileRegex(textArg(a[1]))
			if err != nil {
				return nil, err
			}
			text := textArg(a[0])
			loc := re.FindStringSubmatchIndex(text)
			if loc == nil {
				return text, nil
			}
			out := re.ExpandString(nil, textArg(a[2]), text, loc)
			return limitText(text[:loc[0]] + string(out) + text[loc[1]:])
		}},
		"replaceAll": {min: 3, max: 3, fn: func(_ *formulaEnv, a []any) (any, error) {
			re, err := compileRegex(textArg(a[1]))
			if err != nil {
				return nil, err
			}
			return limitText(re.ReplaceAllString(textArg(a[0]), textArg(a[2])))
		}},
		"repeat": {min: 2, max: 2, fn: func(_ *formulaEnv, a []any) (any, error) {
			n, err := intArg(a[1])
			if err != nil {
				return nil, err
			}
			text := textArg(a[0])
			if n < 0 || n*len(text) > formulaMaxText {
				return nil, errFormulaTooLarge
			}
			return strings.Repeat(text, n), nil
		}},
		"substring": {min: 2, max: 3, fn: func(_ *formulaEnv, a []any) (any, error) {
			r := []rune(textArg(a[0]))
			start, end, err := sliceBounds(len(r), a[1:])
			if err != nil {
				return nil, err
			}
			return string(r[start:end]), nil
		}},
		"slice": {min: 2, max: 3, fn: func(_ *formulaEnv, a []any) (any, error) {
			if l, ok := a[0].([]any); ok {
				start, end, err := sliceBounds(len(l), a[1:])
				if err != nil {
					return nil, err
				}
				return append([]any(nil), l[start:end]...), nil
			}
			r := []rune(textArg(a[0]))
			start, end, err := sliceBounds(len(r), a[1:])
			if err != nil {
				return nil, err
			}
			return string(r[start:end]), nil
		}},
		// dates
		"now": {min: 0, max: 0, fn: func(e *formulaEnv, _ []any) (any, error) {
			return formulaDate{t: e.now.Truncate(time.Minute), hasTime: true}, nil
		}},
		"today": {min: 0, max: 0, fn: func(e *formulaEnv, _ []any) (any, error) {
			y, m, d := e.now.Date()
			return formulaDate{t: time.Date(y, m, d, 0, 0, 0, 0, e.now.Location())}, nil
		}},
		"parseDate": {min: 1, max: 1, fn: func(_ *formulaEnv, a []any) (any, error) {
			d, err := dateArg(a[0])
			if err != nil {
				return nil, fmt.Errorf("cannot read %q as a date", textArg(a[0]))
			}
			return d, nil
		}},
		"dateAdd":      {min: 3, max: 3, fn: func(_ *formulaEnv, a []any) (any, error) { return shiftDate(a, 1) }},
		"dateSubtract": {min: 3, max: 3, fn: func(_ *formulaEnv, a []any) (any, error) { return shiftDate(a, -1) }},
		"dateBetween": {min: 3, max: 3, fn: func(_ *formulaEnv, a []any) (any, error) {
			x, err := dateArg(a[0])
			if err != nil {
				return nil, err
			}
			y, err := dateArg(a[1])
			if err != nil {
				return nil, err
			}
			unit, err := unitArg(a[2])
			if err != nil {
				return nil, err
			}
			return dateBetween(x.t, y.t, unit), nil
		}},
		"formatDate": {min: 2, max: 2, fn: func(_ *formulaEnv, a []any) (any, error) {
			d, err := dateArg(a[0])
			if err != nil {
				return nil, err
			}
			return formatDate(d.t, textArg(a[1])), nil
		}},
		"year":   datePart(func(t time.Time) int { return t.Year() }),
		"month":  datePart(func(t time.Time) int { return int(t.Month()) }),
		"date":   datePart(func(t time.Time) int { return t.Day() }),
		"hour":   datePart(func(t time.Time) int { return t.Hour() }),
		"minute": datePart(func(t time.Time) int { return t.Minute() }),
		"week": datePart(func(t time.Time) int {
			_, w := t.ISOWeek()
			return w
		}),
		// day() is the day of the week, 1 (Monday) to 7 (Sunday).
		"day": datePart(func(t time.Time) int {
			if t.Weekday() == time.Sunday {
				return 7
			}
			return int(t.Weekday())
		}),
		"timestamp": {min: 1, max: 1, fn: func(_ *formulaEnv, a []any) (any, error) {
			d, err := dateArg(a[0])
			if err != nil {
				return nil, err
			}
			return float64(d.t.UnixMilli()), nil
		}},
		"fromTimestamp": {min: 1, max: 1, fn: func(_ *formulaEnv, a []any) (any, error) {
			ms, err := numberArg(a[0])
			if err != nil {
				return nil, err
			}
			return formulaDate{t: time.UnixMilli(int64(ms)).In(time.Local), hasTime: true}, nil
		}},
		// lists
		"at": {min: 2, max: 2, fn: func(_ *formulaEnv, a []any) (any, error) {
			list, err := listArg(a[0])
			if err != nil {
				return nil, err
			}
			i, err := intArg(a[1])
			if err != nil {
				return nil, err
			}
			if i < 0 {
				i += len(list)
			}
			if i < 0 || i >= len(list) {
				return nil, nil
			}
			return list[i], nil
		}},
		"first": {min: 1, max: 1, fn: func(_ *formulaEnv, a []any) (any, error) {
			list, err := listArg(a[0])
			if err != nil || len(list) == 0 {
				return nil, err
			}
			return list[0], nil
		}},
		"last": {min: 1, max: 1, fn: func(_ *formulaEnv, a []any) (any, error) {
			list, err := listArg(a[0])
			if err != nil || len(list) == 0 {
				return nil, err
			}
			return list[len(list)-1], nil
		}},
		"sort": {min: 1, max: 1, fn: func(_ *formulaEnv, a []any) (any, error) {
			list, err := listArg(a[0])
			if err != nil {
				return nil, err
			}
			return sortedCopy(list)
		}},
		"reverse": {min: 1, max: 1, fn: func(_ *formulaEnv, a []any) (any, error) {
			list, err := listArg(a[0])
			if err != nil {
				return nil, err
			}
			out := make([]any, len(list))
			for i, x := range list {
				out[len(list)-1-i] = x
			}
			return out, nil
		}},
		"unique": {min: 1, max: 1, fn: func(_ *formulaEnv, a []any) (any, error) {
			list, err := listArg(a[0])
			if err != nil {
				return nil, err
			}
			return uniqueValues(list), nil
		}},
		"flat": {min: 1, max: 1, fn: func(_ *formulaEnv, a []any) (any, error) {
			list, err := listArg(a[0])
			if err != nil {
				return nil, err
			}
			return flattenValues(list), nil
		}},
		"includes": {min: 2, max: 2, fn: func(e *formulaEnv, a []any) (any, error) {
			return formulaFuncs["contains"].fn(e, a)
		}},
		"map": listFunc(func(e *formulaEnv, list []any, each func(int) (any, error)) (any, error) {
			out := make([]any, len(list))
			for i := range list {
				v, err := each(i)
				if err != nil {
					return nil, err
				}
				out[i] = v
			}
			return out, nil
		}),
		"filter": listFunc(func(e *formulaEnv, list []any, each func(int) (any, error)) (any, error) {
			out := []any{}
			for i := range list {
				v, err := each(i)
				if err != nil {
					return nil, err
				}
				if formulaTruthy(v) {
					out = append(out, list[i])
				}
			}
			return out, nil
		}),
		"find": listFunc(func(e *formulaEnv, list []any, each func(int) (any, error)) (any, error) {
			i, err := findIndex(list, each)
			if err != nil || i < 0 {
				return nil, err
			}
			return list[i], nil
		}),
		"findIndex": listFunc(func(e *formulaEnv, list []any, each func(int) (any, error)) (any, error) {
			i, err := findIndex(list, each)
			return float64(i), err
		}),
		"some": listFunc(func(e *formulaEnv, list []any, each func(int) (any, error)) (any, error) {
			i, err := findIndex(list, each)
			return i >= 0, err
		}),
		"every": listFunc(func(e *formulaEnv, list []any, each func(int) (any, error)) (any, error) {
			for i := range list {
				v, err := each(i)
				if err != nil {
					return nil, err
				}
				if !formulaTruthy(v) {
					return false, nil
				}
			}
			return true, nil
		}),
		// the row itself
		"id": {min: 0, max: 0, fn: func(e *formulaEnv, _ []any) (any, error) { return e.row.id, nil }},
	}
}

func findIndex(list []any, each func(int) (any, error)) (int, error) {
	for i := range list {
		v, err := each(i)
		if err != nil {
			return -1, err
		}
		if formulaTruthy(v) {
			return i, nil
		}
	}
	return -1, nil
}

func datePart(f func(time.Time) int) formulaFunc {
	return formulaFunc{min: 1, max: 1, fn: func(_ *formulaEnv, a []any) (any, error) {
		if a[0] == nil {
			return nil, nil
		}
		d, err := dateArg(a[0])
		if err != nil {
			return nil, err
		}
		return float64(f(d.t)), nil
	}}
}

func shiftDate(a []any, sign float64) (any, error) {
	if a[0] == nil {
		return nil, nil
	}
	d, err := dateArg(a[0])
	if err != nil {
		return nil, err
	}
	n, err := numberArg(a[1])
	if err != nil {
		return nil, err
	}
	unit, err := unitArg(a[2])
	if err != nil {
		return nil, err
	}
	return addToDate(d, sign*n, unit), nil
}

func flattenValues(list []any) []any {
	out := []any{}
	for _, x := range list {
		if inner, ok := x.([]any); ok {
			out = append(out, flattenValues(inner)...)
		} else {
			out = append(out, x)
		}
	}
	return out
}

// uniqueValues keeps the first of every value, in order.
func uniqueValues(list []any) []any {
	out := []any{}
	seen := map[string]bool{}
	for _, x := range list {
		key := typeName(x) + ":" + formatFormulaValue(x)
		if !seen[key] {
			seen[key] = true
			out = append(out, x)
		}
	}
	return out
}
