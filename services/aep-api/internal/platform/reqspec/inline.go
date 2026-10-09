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
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The console reads a requirements line from the collab room, where inline
// markdown is a mark on the text and not characters in it: `name` is the
// word name, **every** is every, [the guide](url) is the guide. The room
// parses the file with `marked` (through @tiptap/markdown and StarterKit),
// and a design's basis is compared with the console's by string equality, so
// this reader takes the same words out of the same source. It covers what the
// room's schema has marks for — code spans, emphasis, strong, strikethrough,
// links (and images, which the schema reads as their alt text), autolinks —
// plus backslash escapes and entity references; anything else is text.
// `marked` is the reference, not the CommonMark spec: the cases both sides
// are held to are packages/contracts/requirements/inline-markup-cases.json.
//
// Requirement files are user-written and aep-api reads every one of them per
// basis, so the reader is linear in the line's length whatever its markup:
// brackets, parentheses and code spans are paired in one pass up front, and
// emphasis is paired against a stack with CommonMark's openers_bottom, so no
// delimiter or bracket rescans the line.

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
	text string

	delim             byte
	n, orig           int // delimiter characters left, and at first
	canOpen, canClose bool
	opensIt, closesIt int // italic emphasis opened / closed here
}

var (
	autolinkRE = regexp.MustCompile(`^<([A-Za-z][A-Za-z0-9+.\-]{1,31}:[^\s<>]*)>`)
	emailRE    = regexp.MustCompile(`^<([A-Za-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~\-]+@[A-Za-z0-9](?:[A-Za-z0-9\-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9\-]{0,61}[A-Za-z0-9])?)*)>`)
)

// readInline reads one line of markdown as the text the room holds for it.
func readInline(s string) inlineText {
	r := &reader{s: s, code: backtickRuns(s), bottom: map[bottomKey]int{}}
	r.links = r.findLinks()
	r.read()
	return render(r.pieces)
}

type reader struct {
	s string
	// code is every backtick run's start, by run length, in order.
	code map[int][]int
	// links maps a link's `[` to its label's `]` and the end of its `(…)`.
	links map[int]linkSpan

	pieces []piece
	raw    strings.Builder // text not yet flushed; entity references still in it
	// stack holds the delimiter runs that may still open, as piece indices.
	stack []int
	// bottom is CommonMark's openers_bottom: for a kind of closer, the stack
	// height below which no opener for it can be (it was searched already).
	bottom map[bottomKey]int
	// frames are the links being read, innermost last: emphasis inside a
	// label pairs only inside it.
	frames []frame
}

type linkSpan struct{ labelEnd, end int }

type frame struct {
	linkSpan
	labelStart int
	floor      int // stack height when the label opened
}

// bottomKey is what decides whether a closer can pair with an opener: its
// character, and for the rule of three whether it can open and its length
// mod 3 (for `~`, its length).
type bottomKey struct {
	delim   byte
	canOpen bool
	length  int
}

// skip is how far an escape, code span or autolink at i reaches, or 0 when
// none starts there. The pairing pass and the read take the same jumps, so
// both see the same brackets.
func (r *reader) skip(i int) (end int, text string) {
	s := r.s
	switch s[i] {
	case '\\':
		if i+1 < len(s) && isASCIIPunct(s[i+1]) {
			return i + 2, s[i+1 : i+2]
		}
	case '`':
		run := runLength(s, i, '`')
		if close := r.closingRun(i, run); close >= 0 {
			content := s[i+run : close]
			if len(content) >= 2 && content[0] == ' ' && content[len(content)-1] == ' ' && strings.TrimSpace(content) != "" {
				content = content[1 : len(content)-1]
			}
			return close + run, content
		}
		return i + run, s[i : i+run]
	case '<':
		if m := autolinkRE.FindStringSubmatch(s[i:]); m != nil {
			return i + len(m[0]), m[1]
		}
		if m := emailRE.FindStringSubmatch(s[i:]); m != nil {
			return i + len(m[0]), m[1]
		}
	}
	return 0, ""
}

// closingRun is where the backtick run of length run opening at i closes:
// the next run of exactly that length, or -1.
func (r *reader) closingRun(i, run int) int {
	starts := r.code[run]
	k := sort.SearchInts(starts, i+run)
	if k < len(starts) {
		return starts[k]
	}
	return -1
}

// findLinks pairs every inline link `[label](destination)` in one pass. A
// bracket with no `(` after its match (a source tag, `[T&E Policy · p.3]`)
// is not a link.
func (r *reader) findLinks() map[int]linkSpan {
	s := r.s
	parens := matchParens(s)
	links := map[int]linkSpan{}
	var open []int
	for i := 0; i < len(s); {
		if end, _ := r.skip(i); end > 0 {
			i = end
			continue
		}
		switch s[i] {
		case '[':
			open = append(open, i)
		case ']':
			if len(open) > 0 {
				start := open[len(open)-1]
				open = open[:len(open)-1]
				if close, ok := parens[i+1]; ok {
					links[start] = linkSpan{labelEnd: i, end: close + 1}
					i = close + 1
					continue
				}
			}
		}
		i++
	}
	return links
}

// matchParens pairs each `(` with its `)`, nesting counted and backslash
// escapes skipped.
func matchParens(s string) map[int]int {
	out := map[int]int{}
	var open []int
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '(':
			open = append(open, i)
		case ')':
			if len(open) > 0 {
				out[open[len(open)-1]] = i
				open = open[:len(open)-1]
			}
		}
	}
	return out
}

func backtickRuns(s string) map[int][]int {
	runs := map[int][]int{}
	for i := 0; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		n := runLength(s, i, '`')
		runs[n] = append(runs[n], i)
		i += n
	}
	return runs
}

func runLength(s string, i int, c byte) int {
	n := 0
	for i+n < len(s) && s[i+n] == c {
		n++
	}
	return n
}

func (r *reader) flush() {
	if r.raw.Len() > 0 {
		r.pieces = append(r.pieces, piece{text: html.UnescapeString(r.raw.String())})
		r.raw.Reset()
	}
}

func (r *reader) literal(text string) {
	r.flush()
	r.pieces = append(r.pieces, piece{text: text})
}

// read splits the line into text and delimiter runs, pairing emphasis as it
// goes.
func (r *reader) read() {
	s := r.s
	for i := 0; i < len(s); {
		if f := len(r.frames) - 1; f >= 0 && i == r.frames[f].labelEnd {
			i = r.closeFrame()
			continue
		}
		if end, text := r.skip(i); end > 0 {
			r.literal(text)
			i = end
			continue
		}
		c := s[i]
		switch {
		case c == '[' || (c == '!' && i+1 < len(s) && s[i+1] == '['):
			open := i
			if c == '!' {
				open++
			}
			if l, ok := r.links[open]; ok {
				r.frames = append(r.frames, frame{linkSpan: l, labelStart: open + 1, floor: len(r.stack)})
			} else {
				r.raw.WriteString(s[i : open+1])
			}
			i = open + 1
		case c == '*' || c == '_' || c == '~':
			r.flush()
			run := runLength(s, i, c)
			r.delimiter(i, run)
			i += run
		default:
			r.raw.WriteByte(c)
			i++
		}
	}
	r.flush()
}

// closeFrame ends the innermost link at its label's `]`: its unpaired
// delimiters stay text, and the read resumes after its destination.
func (r *reader) closeFrame() int {
	f := r.frames[len(r.frames)-1]
	r.frames = r.frames[:len(r.frames)-1]
	r.truncate(f.floor)
	return f.end
}

// delimiter classifies the run of n delimiter characters at i by the
// CommonMark flanking rules, pairs it as a closer, and keeps what is left as
// a possible opener. Inside a link label the label's ends count as the
// line's: `marked` reads a label as a line of its own.
func (r *reader) delimiter(i, n int) {
	s := r.s
	lo, hi := 0, len(s)
	if f := len(r.frames) - 1; f >= 0 {
		lo, hi = r.frames[f].labelStart, r.frames[f].labelEnd
	}
	before, after := ' ', ' '
	if i > lo {
		before, _ = utf8.DecodeLastRuneInString(s[:i])
	}
	if i+n < hi {
		after, _ = utf8.DecodeRuneInString(s[i+n:])
	}
	left := !unicode.IsSpace(after) && (!isPunct(after) || unicode.IsSpace(before) || isPunct(before))
	right := !unicode.IsSpace(before) && (!isPunct(before) || unicode.IsSpace(after) || isPunct(after))
	p := piece{delim: s[i], n: n, orig: n, canOpen: left, canClose: right}
	switch s[i] {
	case '_':
		p.canOpen = left && (!right || isPunct(before))
		p.canClose = right && (!left || isPunct(after))
	case '~':
		// GFM strikethrough: one or two tildes; a longer run is text.
		if n > 2 {
			p.canOpen, p.canClose = false, false
		}
	}
	idx := len(r.pieces)
	r.pieces = append(r.pieces, p)
	if p.canClose {
		r.close(idx)
	}
	if q := r.pieces[idx]; q.canOpen && q.n > 0 {
		r.stack = append(r.stack, idx)
	}
}

// close pairs the closer at idx with the nearest opener of its kind
// (CommonMark "process emphasis"); the delimiters between them can no longer
// pair. A search that finds nothing records its bottom, so no later closer of
// the same kind searches that part of the stack again.
func (r *reader) close(idx int) {
	closer := &r.pieces[idx]
	for closer.n > 0 {
		key := bottomKey{delim: closer.delim, canOpen: closer.canOpen, length: closer.orig % 3}
		if closer.delim == '~' {
			key.length = closer.orig
		}
		floor := r.bottom[key]
		if f := len(r.frames) - 1; f >= 0 {
			floor = max(floor, r.frames[f].floor)
		}
		at := -1
		for k := len(r.stack) - 1; k >= floor; k-- {
			if pairs(r.pieces[r.stack[k]], *closer) {
				at = k
				break
			}
		}
		if at < 0 {
			r.bottom[key] = len(r.stack)
			return
		}
		open := &r.pieces[r.stack[at]]
		use := 1
		switch {
		case closer.delim == '~':
			use = closer.n
		case open.n >= 2 && closer.n >= 2:
			use = 2
		}
		if use == 1 && closer.delim != '~' {
			open.opensIt++
			closer.closesIt++
		}
		open.n -= use
		closer.n -= use
		if open.n == 0 {
			r.truncate(at)
		} else {
			r.truncate(at + 1)
		}
	}
}

// pairs reports whether an opener on the stack pairs with the closer.
func pairs(open, closer piece) bool {
	if open.delim != closer.delim {
		return false
	}
	if closer.delim == '~' {
		return open.n == closer.n
	}
	// The rule of three: `*a**b*` is not `a` emphasised then `b`.
	return !((open.canClose || closer.canOpen) && (open.orig+closer.orig)%3 == 0 && (open.orig%3 != 0 || closer.orig%3 != 0))
}

// truncate drops the stack above height, keeping every bottom within it.
func (r *reader) truncate(height int) {
	r.stack = r.stack[:height]
	for k, b := range r.bottom {
		if b > height {
			r.bottom[k] = height
		}
	}
}

// render joins the pieces into the line's text: a delimiter's unused
// characters stay as text, and what lies between an italic pair is italic.
func render(pieces []piece) inlineText {
	var b strings.Builder
	var italic []span
	depth := 0
	for _, p := range pieces {
		text := p.text
		if p.delim != 0 {
			depth -= p.closesIt
			text = strings.Repeat(string(p.delim), p.n)
		}
		start := b.Len()
		b.WriteString(text)
		if depth > 0 && b.Len() > start {
			if last := len(italic) - 1; last >= 0 && italic[last].end >= start {
				italic[last].end = b.Len()
			} else {
				italic = append(italic, span{start, b.Len()})
			}
		}
		if p.delim != 0 {
			depth += p.opensIt
		}
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
