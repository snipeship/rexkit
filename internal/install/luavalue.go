package install

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Settings files are plain Lua data: `return { key = value, ... }`, perhaps
// via `local M = {...} return M`. This file reads and writes that subset
// (strings, numbers, booleans, nil and nested tables) without running Lua.

// luaTable is a Lua table constructor: positional items and named fields in
// source order. A field written as `key = nil` is kept with a nil value, so
// a defaults file can declare a setting whose default is "unset".
type luaTable struct {
	Items  []any
	Keys   []string
	Fields map[string]any
}

// Get returns a named field and whether the table declares it.
func (t *luaTable) Get(key string) (any, bool) {
	v, ok := t.Fields[key]
	return v, ok
}

func (t *luaTable) set(key string, v any) {
	if t.Fields == nil {
		t.Fields = map[string]any{}
	}
	if _, seen := t.Fields[key]; !seen {
		t.Keys = append(t.Keys, key)
	}
	t.Fields[key] = v
}

// parseLuaData parses a Lua chunk that returns a data table.
func parseLuaData(src string) (*luaTable, error) {
	p := &luaParser{src: src}
	locals := map[string]any{}
	for {
		p.space()
		switch {
		case p.word("local"):
			name := p.name()
			if name == "" || !p.symbol("=") {
				return nil, p.errorf("expected `local NAME = value`")
			}
			v, err := p.value(locals)
			if err != nil {
				return nil, err
			}
			locals[name] = v
		case p.word("return"):
			v, err := p.value(locals)
			if err != nil {
				return nil, err
			}
			p.symbol(";")
			if p.space(); p.pos < len(p.src) {
				return nil, p.errorf("unexpected text after return")
			}
			t, ok := v.(*luaTable)
			if !ok {
				return nil, p.errorf("the file must return a table")
			}
			return t, nil
		default:
			return nil, p.errorf("expected `return { ... }`")
		}
	}
}

type luaParser struct {
	src string
	pos int
}

func (p *luaParser) errorf(format string, args ...any) error {
	line := 1 + strings.Count(p.src[:p.pos], "\n")
	return fmt.Errorf("line %d: %s", line, fmt.Sprintf(format, args...))
}

// space skips whitespace and comments.
func (p *luaParser) space() {
	for p.pos < len(p.src) {
		switch c := p.src[p.pos]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			p.pos++
		case strings.HasPrefix(p.src[p.pos:], "--"):
			p.pos += 2
			if level, ok := p.longOpen(); ok {
				p.longBody(level)
				continue
			}
			for p.pos < len(p.src) && p.src[p.pos] != '\n' {
				p.pos++
			}
		default:
			return
		}
	}
}

func (p *luaParser) symbol(s string) bool {
	p.space()
	if strings.HasPrefix(p.src[p.pos:], s) {
		p.pos += len(s)
		return true
	}
	return false
}

// word consumes a keyword only when it is not the prefix of a longer name.
func (p *luaParser) word(w string) bool {
	p.space()
	rest := p.src[p.pos:]
	if strings.HasPrefix(rest, w) && (len(rest) == len(w) || !isNameByte(rest[len(w)])) {
		p.pos += len(w)
		return true
	}
	return false
}

func (p *luaParser) name() string {
	p.space()
	start := p.pos
	for p.pos < len(p.src) && isNameByte(p.src[p.pos]) && (p.pos > start || !isDigit(p.src[p.pos])) {
		p.pos++
	}
	return p.src[start:p.pos]
}

func (p *luaParser) value(locals map[string]any) (any, error) {
	p.space()
	if p.pos >= len(p.src) {
		return nil, p.errorf("unexpected end of file")
	}
	switch c := p.src[p.pos]; {
	case c == '{':
		return p.table(locals)
	case c == '"' || c == '\'':
		return p.shortString()
	case c == '[':
		level, ok := p.longOpen()
		if !ok {
			return nil, p.errorf("unexpected [")
		}
		return p.longBody(level), nil
	case c == '-' || c == '.' || isDigit(c):
		return p.number()
	}
	switch {
	case p.word("true"):
		return true, nil
	case p.word("false"):
		return false, nil
	case p.word("nil"):
		return nil, nil
	}
	if name := p.name(); name != "" {
		if v, ok := locals[name]; ok {
			return v, nil
		}
		return nil, p.errorf("%s is not a plain value", name)
	}
	return nil, p.errorf("unexpected %q", p.src[p.pos])
}

func (p *luaParser) table(locals map[string]any) (*luaTable, error) {
	p.pos++ // {
	t := &luaTable{}
	for {
		if p.symbol("}") {
			return t, nil
		}
		key, err := p.fieldKey(locals)
		if err != nil {
			return nil, err
		}
		v, err := p.value(locals)
		if err != nil {
			return nil, err
		}
		if key == nil {
			t.Items = append(t.Items, v)
		} else {
			t.set(*key, v)
		}
		if !p.symbol(",") && !p.symbol(";") {
			if p.symbol("}") {
				return t, nil
			}
			return nil, p.errorf("expected , or } in table")
		}
	}
}

// fieldKey consumes `name =` or `["key"] =` and returns the key, or nil for a
// positional item.
func (p *luaParser) fieldKey(locals map[string]any) (*string, error) {
	p.space()
	start := p.pos
	if start >= len(p.src) {
		return nil, p.errorf("unfinished table")
	}
	if p.src[p.pos] == '[' {
		if _, long := p.longOpen(); long {
			p.pos = start
			return nil, nil
		}
		p.pos++
		k, err := p.value(locals)
		if err != nil {
			return nil, err
		}
		key, ok := k.(string)
		if !ok || !p.symbol("]") || !p.symbol("=") {
			return nil, p.errorf("only string keys are supported")
		}
		return &key, nil
	}
	if name := p.name(); name != "" && !isKeyword(name) {
		if p.symbol("=") && !strings.HasPrefix(p.src[p.pos:], "=") {
			return &name, nil
		}
	}
	p.pos = start
	return nil, nil
}

func (p *luaParser) number() (float64, error) {
	start := p.pos
	neg := p.symbol("-")
	p.space()
	numStart := p.pos
	for p.pos < len(p.src) && strings.IndexByte("0123456789abcdefABCDEFxX.+-pP", p.src[p.pos]) >= 0 {
		if (p.src[p.pos] == '+' || p.src[p.pos] == '-') && !strings.ContainsRune("eEpP", rune(p.src[p.pos-1])) {
			break
		}
		p.pos++
	}
	text := p.src[numStart:p.pos]
	var f float64
	var err error
	if strings.HasPrefix(strings.ToLower(text), "0x") {
		var n int64
		n, err = strconv.ParseInt(text[2:], 16, 64)
		f = float64(n)
	} else {
		f, err = strconv.ParseFloat(text, 64)
	}
	if err != nil || text == "" {
		p.pos = start
		return 0, p.errorf("bad number %q", text)
	}
	if neg {
		f = -f
	}
	return f, nil
}

func (p *luaParser) shortString() (string, error) {
	quote := p.src[p.pos]
	p.pos++
	var b strings.Builder
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		p.pos++
		switch {
		case c == quote:
			return b.String(), nil
		case c == '\n':
			return "", p.errorf("unfinished string")
		case c != '\\':
			b.WriteByte(c)
			continue
		}
		if p.pos >= len(p.src) {
			break
		}
		e := p.src[p.pos]
		p.pos++
		if r, ok := simpleEscapes[e]; ok {
			b.WriteByte(r)
			continue
		}
		switch {
		case e == '\n':
			b.WriteByte('\n')
		case e == 'z':
			for p.pos < len(p.src) && strings.IndexByte(" \t\r\n", p.src[p.pos]) >= 0 {
				p.pos++
			}
		case e == 'x' && p.pos+2 <= len(p.src):
			n, err := strconv.ParseUint(p.src[p.pos:p.pos+2], 16, 8)
			if err != nil {
				return "", p.errorf("bad \\x escape")
			}
			b.WriteByte(byte(n))
			p.pos += 2
		case isDigit(e):
			end := p.pos
			for end < len(p.src) && end < p.pos+2 && isDigit(p.src[end]) {
				end++
			}
			n, err := strconv.ParseUint(p.src[p.pos-1:end], 10, 16)
			if err != nil || n > 255 {
				return "", p.errorf("bad decimal escape")
			}
			b.WriteByte(byte(n))
			p.pos = end
		case e == 'u' && strings.HasPrefix(p.src[p.pos:], "{"):
			end := strings.IndexByte(p.src[p.pos:], '}')
			n, err := strconv.ParseUint(p.src[p.pos+1:p.pos+max(end, 1)], 16, 32)
			if end < 0 || err != nil {
				return "", p.errorf("bad \\u escape")
			}
			b.WriteRune(rune(n))
			p.pos += end + 1
		default:
			return "", p.errorf("bad escape \\%c", e)
		}
	}
	return "", p.errorf("unfinished string")
}

var simpleEscapes = map[byte]byte{
	'a': '\a', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t', 'v': '\v',
	'\\': '\\', '"': '"', '\'': '\'',
}

// longOpen consumes `[[`, `[=[`, ... and returns the level.
func (p *luaParser) longOpen() (int, bool) {
	rest := p.src[p.pos:]
	if !strings.HasPrefix(rest, "[") {
		return 0, false
	}
	level := 1
	for level < len(rest) && rest[level] == '=' {
		level++
	}
	if level >= len(rest) || rest[level] != '[' {
		return 0, false
	}
	p.pos += level + 1
	return level - 1, true
}

// longBody consumes up to the matching close bracket and returns the text,
// minus a first newline (as Lua does).
func (p *luaParser) longBody(level int) string {
	closer := "]" + strings.Repeat("=", level) + "]"
	rest := p.src[p.pos:]
	end := strings.Index(rest, closer)
	if end < 0 {
		p.pos = len(p.src)
		return rest
	}
	p.pos += end + len(closer)
	body := rest[:end]
	if strings.HasPrefix(body, "\r\n") {
		return body[2:]
	}
	return strings.TrimPrefix(body, "\n")
}

func isDigit(c byte) bool    { return c >= '0' && c <= '9' }
func isNameByte(c byte) bool { return c == '_' || isDigit(c) || c|0x20 >= 'a' && c|0x20 <= 'z' }

var luaKeywords = []string{"and", "break", "do", "else", "elseif", "end", "false", "for", "function",
	"goto", "if", "in", "local", "nil", "not", "or", "repeat", "return", "then", "true", "until", "while"}

func isKeyword(s string) bool { return slices.Contains(luaKeywords, s) }

// luaEqual compares two parsed values.
func luaEqual(a, b any) bool {
	switch a := a.(type) {
	case *luaTable:
		b, ok := b.(*luaTable)
		if !ok || len(a.Items) != len(b.Items) || len(a.Keys) != len(b.Keys) {
			return false
		}
		for i := range a.Items {
			if !luaEqual(a.Items[i], b.Items[i]) {
				return false
			}
		}
		for _, k := range a.Keys {
			bv, ok := b.Get(k)
			if !ok || !luaEqual(a.Fields[k], bv) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}

// formatLua writes v as Lua source, indenting nested tables by depth.
func formatLua(v any, depth int) string {
	switch v := v.(type) {
	case nil:
		return "nil"
	case bool:
		return strconv.FormatBool(v)
	case float64:
		if v == math.Trunc(v) && math.Abs(v) < 1e15 {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'g', -1, 64)
	case string:
		return luaString(v)
	case *luaTable:
		return formatTable(v, depth)
	}
	panic(fmt.Sprintf("install: cannot format %T as Lua", v))
}

func formatTable(t *luaTable, depth int) string {
	if len(t.Keys) == 0 {
		parts := make([]string, len(t.Items))
		for i, item := range t.Items {
			parts[i] = formatLua(item, depth+1)
		}
		if len(parts) == 0 {
			return "{}"
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	}
	indent := strings.Repeat("  ", depth+1)
	var b strings.Builder
	b.WriteString("{\n")
	for _, item := range t.Items {
		b.WriteString(indent + formatLua(item, depth+1) + ",\n")
	}
	for _, k := range t.Keys {
		b.WriteString(indent + luaKey(k) + " = " + formatLua(t.Fields[k], depth+1) + ",\n")
	}
	b.WriteString(strings.Repeat("  ", depth) + "}")
	return b.String()
}

func luaKey(k string) string {
	if k != "" && !isDigit(k[0]) && !isKeyword(k) && strings.IndexFunc(k, func(r rune) bool { return r > 127 || !isNameByte(byte(r)) }) < 0 {
		return k
	}
	return "[" + luaString(k) + "]"
}

// luaString quotes s as a Lua short string.
func luaString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == utf8.RuneError && size == 1, r < 0x20, r == 0x7f:
			fmt.Fprintf(&b, `\%03d`, s[i])
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	b.WriteByte('"')
	return b.String()
}

// longBracket quotes s as a Lua long string whose level guarantees that s
// cannot close it early.
func longBracket(s string) string {
	eq := ""
	for strings.Contains(s+"]", "]"+eq+"]") {
		eq += "="
	}
	return "[" + eq + "[" + s + "]" + eq + "]"
}

// luaComment makes s safe to write after `--` in a line comment: control
// characters, which could end the comment and turn the rest into code, are
// shown as escapes instead.
func luaComment(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
