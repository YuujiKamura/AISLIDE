package cp

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
)

// token: 引用符を外した 1 語。k=v 形式なら key/val も持つ。
type token struct {
	text     string // 引用符を外した全体（k=v なら "k=v"）
	key, val string
	isKV     bool
}

type command struct {
	verb string
	toks []token // verb 以降
	pos  []string
	kv   map[string]string
}

var keyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// parseCommand: AICAD 互換。空白区切り、"..." で空白を含む値、k=v は kv に。
func parseCommand(line string) (*command, error) {
	line = strings.TrimPrefix(line, string(rune(0xFEFF)))
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, errors.New("empty command")
	}
	words, err := splitWords(line)
	if err != nil {
		return nil, err
	}
	c := &command{verb: strings.ToLower(words[0]), kv: map[string]string{}}
	for _, w := range words[1:] {
		t := token{text: w}
		if i := strings.IndexByte(w, '='); i > 0 && keyRe.MatchString(w[:i]) {
			t.isKV, t.key, t.val = true, w[:i], w[i+1:]
			c.kv[t.key] = t.val
		} else {
			c.pos = append(c.pos, w)
		}
		c.toks = append(c.toks, t)
	}
	return c, nil
}

func splitWords(s string) ([]string, error) {
	var out []string
	var b strings.Builder
	inQ, have := false, false
	for _, r := range s {
		switch {
		case r == '"':
			inQ = !inQ
			have = true
		case !inQ && unicode.IsSpace(r):
			if have {
				out = append(out, b.String())
				b.Reset()
				have = false
			}
		default:
			b.WriteRune(r)
			have = true
		}
	}
	if inQ {
		return nil, errors.New("unterminated quote")
	}
	if have {
		out = append(out, b.String())
	}
	return out, nil
}

// agent: agent= 引数（必須のコマンドでは空なら ng）。
func (c *command) agent() string { return c.kv["agent"] }

// argsExcept: kv から除外キーを抜いたもの（parts.Build へ渡す）。
func (c *command) argsExcept(keys ...string) map[string]string {
	m := make(map[string]string, len(c.kv))
	for k, v := range c.kv {
		m[k] = v
	}
	for _, k := range keys {
		delete(m, k)
	}
	return m
}
