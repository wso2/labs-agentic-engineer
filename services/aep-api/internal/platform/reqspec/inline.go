// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package reqspec

import (
	"html"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The console reads a requirements line from the collab room, where inline
// markdown is a mark on the text and not characters in it: `name` is the
// word name, **every** is every, [the guide](url) is the guide. The room
// parses the file with CommonMark + GFM (@tiptap/markdown, StarterKit), and a
// design's basis is compared with the console's by string equality, so this
// reader takes the same words out of the same source. It covers what the
// room's schema has marks for — code spans, emphasis, strong, strikethrough,
// links (and images, which the schema reads as their alt text), autolinks —
// plus backslash escapes and entity references. Anything else is text.

// inlineText is a line's text with the runs the room would mark italic, as
// byte offsets into text.
type inlineText struct {
	text   string
	italic []span
}

type span struct{ start, end int }

// piece is a run of a line being read: text, or a delimiter run (`*`, `_`,
// `~`) that may turn out to be emphasis or strikethrough.
type piece struct {
	text   string
	italic []span // italic runs inside text (a link's own emphasis)

	delim             byte
	n, orig           int // delimiter characters left, and at first
	canOpen, canClose bool
	active            bool
	opensIt, closesIt int // italic emphasis opened / closed here
}

var (
	autolinkRE = regexp.MustCompile(`^<([A-Za-z][A-Za-z0-9+.\-]{1,31}:[^\s<>]*)>`)
	emailRE    = regexp.MustCompile(`^<([A-Za-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~\-]+@[A-Za-z0-9](?:[A-Za-z0-9\-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9\-]{0,61}[A-Za-z0-9])?)*)>`)
)

// readInline reads one line of markdown as the text the room holds for it.
func readInline(s string) inlineText {
	pieces := tokenize(s)
	matchDelimiters(pieces)
	return render(pieces)
}

// tokenize splits a line into text and delimiter runs. Code spans,
// autolinks and links are read whole here: nothing outside one pairs with a
// delimiter inside it.
func tokenize(s string) []piece {
	var out []piece
	var raw strings.Builder // text not yet flushed; entity references still in it
	flush := func() {
		if raw.Len() > 0 {
			out = append(out, piece{text: html.UnescapeString(raw.String())})
			raw.Reset()
		}
	}
	literal := func(text string, italic []span) {
		flush()
		out = append(out, piece{text: text, italic: italic})
	}
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && isASCIIPunct(s[i+1]):
			literal(s[i+1:i+2], nil)
			i += 2
		case c == '`':
			run := runLength(s, i, '`')
			if content, end, ok := codeSpan(s, i, run); ok {
				literal(content, nil)
				i = end
			} else {
				literal(s[i:i+run], nil)
				i += run
			}
		case c == '<':
			if m := autolinkRE.FindStringSubmatch(s[i:]); m != nil {
				literal(m[1], nil)
				i += len(m[0])
			} else if m := emailRE.FindStringSubmatch(s[i:]); m != nil {
				literal(m[1], nil)
				i += len(m[0])
			} else {
				raw.WriteByte(c)
				i++
			}
		case c == '[' || (c == '!' && i+1 < len(s) && s[i+1] == '['):
			open := i
			if c == '!' {
				open++
			}
			if label, end, ok := link(s, open); ok {
				inner := readInline(label)
				literal(inner.text, inner.italic)
				i = end
			} else {
				raw.WriteString(s[i : open+1])
				i = open + 1
			}
		case c == '*' || c == '_' || c == '~':
			flush()
			run := runLength(s, i, c)
			out = append(out, delimiterRun(s, i, run))
			i += run
		default:
			raw.WriteByte(c)
			i++
		}
	}
	flush()
	return out
}

func runLength(s string, i int, c byte) int {
	n := 0
	for i+n < len(s) && s[i+n] == c {
		n++
	}
	return n
}

// codeSpan reads the code span opening with the backtick run at i: its
// content, taken literally, and where it ends. ok is false when no closing
// run of the same length follows.
func codeSpan(s string, i, run int) (content string, end int, ok bool) {
	for j := i + run; j < len(s); {
		if s[j] != '`' {
			j++
			continue
		}
		n := runLength(s, j, '`')
		if n == run {
			content = s[i+run : j]
			if len(content) >= 2 && content[0] == ' ' && content[len(content)-1] == ' ' && strings.TrimSpace(content) != "" {
				content = content[1 : len(content)-1]
			}
			return content, j + n, true
		}
		j += n
	}
	return "", 0, false
}

// link reads an inline link `[label](destination)` whose `[` is at open: its
// label and where it ends. A bracket with no `(` after its match (a source
// tag, `[T&E Policy · p.3]`) is not a link.
func link(s string, open int) (label string, end int, ok bool) {
	closing := -1
	depth := 0
scan:
	for j := open + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case '`':
			run := runLength(s, j, '`')
			if _, e, ok := codeSpan(s, j, run); ok {
				j = e - 1
			} else {
				j += run - 1
			}
		case '[':
			depth++
		case ']':
			if depth == 0 {
				closing = j
				break scan
			}
			depth--
		}
	}
	if closing < 0 || closing+1 >= len(s) || s[closing+1] != '(' {
		return "", 0, false
	}
	parens := 0
	for j := closing + 2; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case '(':
			parens++
		case ')':
			if parens == 0 {
				return s[open+1 : closing], j + 1, true
			}
			parens--
		}
	}
	return "", 0, false
}

// delimiterRun classifies the run of n delimiter characters at i by the
// CommonMark flanking rules, which decide whether it can open or close.
func delimiterRun(s string, i, n int) piece {
	before, after := ' ', ' '
	if i > 0 {
		before, _ = utf8.DecodeLastRuneInString(s[:i])
	}
	if i+n < len(s) {
		after, _ = utf8.DecodeRuneInString(s[i+n:])
	}
	left := !unicode.IsSpace(after) && (!isPunct(after) || unicode.IsSpace(before) || isPunct(before))
	right := !unicode.IsSpace(before) && (!isPunct(before) || unicode.IsSpace(after) || isPunct(after))
	p := piece{delim: s[i], n: n, orig: n, active: true, canOpen: left, canClose: right}
	switch s[i] {
	case '_':
		p.canOpen = left && (!right || isPunct(before))
		p.canClose = right && (!left || isPunct(after))
	case '~':
		// GFM strikethrough: one or two tildes; a longer run is text.
		p.active = n <= 2
	}
	return p
}

// matchDelimiters pairs openers with closers (CommonMark "process
// emphasis"): each closer takes the nearest opener of its kind, and the
// delimiters between them can no longer pair.
func matchDelimiters(pieces []piece) {
	for i := range pieces {
		closer := &pieces[i]
		for closer.delim != 0 && closer.active && closer.canClose && closer.n > 0 {
			j := opener(pieces, i)
			if j < 0 {
				break
			}
			open := &pieces[j]
			use := 1
			if closer.delim == '~' {
				use = closer.n
			} else if open.n >= 2 && closer.n >= 2 {
				use = 2
			}
			if use == 1 && closer.delim != '~' {
				open.opensIt++
				closer.closesIt++
			}
			open.n -= use
			closer.n -= use
			for k := j + 1; k < i; k++ {
				pieces[k].active = false
			}
			if open.n == 0 {
				open.active = false
			}
		}
	}
}

// opener is the index of the nearest opener before i that the closer at i
// pairs with, or -1.
func opener(pieces []piece, i int) int {
	closer := pieces[i]
	for j := i - 1; j >= 0; j-- {
		p := pieces[j]
		if p.delim != closer.delim || !p.active || !p.canOpen || p.n == 0 {
			continue
		}
		if closer.delim == '~' {
			if p.n == closer.n {
				return j
			}
			continue
		}
		// The rule of three: `*a**b*` is not `a` emphasised then `b`.
		if (p.canClose || closer.canOpen) && (p.orig+closer.orig)%3 == 0 && (p.orig%3 != 0 || closer.orig%3 != 0) {
			continue
		}
		return j
	}
	return -1
}

// render joins the pieces into the line's text: a delimiter's unused
// characters stay as text, and what lies between an italic pair is italic.
func render(pieces []piece) inlineText {
	var b strings.Builder
	var italic []span
	mark := func(start, end int) {
		if start == end {
			return
		}
		if last := len(italic) - 1; last >= 0 && italic[last].end >= start {
			italic[last].end = max(italic[last].end, end)
			return
		}
		italic = append(italic, span{start, end})
	}
	depth := 0
	write := func(text string, inner []span) {
		start := b.Len()
		b.WriteString(text)
		if depth > 0 {
			mark(start, b.Len())
		}
		for _, r := range inner {
			mark(start+r.start, start+r.end)
		}
	}
	for _, p := range pieces {
		if p.delim == 0 {
			write(p.text, p.italic)
			continue
		}
		depth -= p.closesIt
		write(strings.Repeat(string(p.delim), p.n), nil)
		depth += p.opensIt
	}
	return inlineText{text: b.String(), italic: italic}
}

func isASCIIPunct(c byte) bool {
	return c >= '!' && c <= '/' || c >= ':' && c <= '@' || c >= '[' && c <= '`' || c >= '{' && c <= '~'
}

// isPunct is CommonMark's Unicode punctuation: categories P and S.
func isPunct(r rune) bool {
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}
