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

package observability

// cycle_logs.go — one coding cycle's lines, read by Component UID.
//
// WHAT THE OBSERVER GIVES US (OpenChoreo 1.2.5 + its OpenSearch logs adapter):
// a window matched `gt startTime, lt endTime` at WHOLE-SECOND precision, a sort
// on the timestamp alone, at most 1000 lines a page, timestamps returned to the
// second, `total` = the match count of that request's window, and no cursor.
// So a page that ends inside second S cannot say where in S it stopped.
//
// HOW A READ PAGES ON THAT ("boundary-second replacement"):
//   - A full page whose last line is in second S keeps only its lines before S;
//     the next request starts at S-1s (so `gt` admits all of S) and drops what
//     comes back from before S. Every second is then taken from ONE page, whole,
//     so nothing is read twice and no line identity is needed.
//   - A page that holds the whole of its window (shorter than the limit, or as
//     long as `total`) is the last.
//   - A page with nothing before S (S, plus the already-kept tail of S-1s, fill
//     it) cannot move forward that way. S is then read once more from its other
//     end (`desc`), the two reads are joined by LINE IDENTITY (second, pod,
//     Component UID, text — the response carries no document id), and the read
//     moves past S. The join cannot be positional: OpenSearch sorts on the
//     timestamp alone and breaks ties by document order ascending in BOTH
//     directions, so a same-millisecond group is not mirrored between the two
//     reads. Identical lines in one second are indistinguishable, so the join
//     keeps the larger of the two reads' counts of each; with `total` the read
//     knows how many lines of S it still lacks (more than two pages in S, or
//     identical lines) and reports them missing, never duplicated. Moving past
//     S means starting `gt S+1s`, which also skips a line stamped exactly on
//     S+1s.000 — the price of a second-granular window, paid only there.
//
// Paging decisions use every line returned; the Component-UID filter applies to
// the result only, so a project page full of other Components' lines still
// pages on.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// maxCycleLogPages bounds one read: 20 requests is up to 20k lines, far past a
// cycle's measured volume, and the stop that keeps a misbehaving backend from
// turning one read into an unbounded loop.
const maxCycleLogPages = 20

// runnerLinePhrase narrows a project-scope read to the coding runner's NDJSON
// event lines, every one of which carries the event-schema version. The other
// Components of the project then mostly stop sharing its pages.
const runnerLinePhrase = `"v":2`

// pageLine is one returned line with its parsed second.
type pageLine struct {
	at   time.Time
	line LogLine
}

func (q CycleLogQuery) validate() error {
	var missing []string
	for _, f := range [...]struct{ name, v string }{
		{"namespace", q.Namespace}, {"project", q.Project}, {"environment", q.Environment}, {"componentUid", q.ComponentUID},
	} {
		if f.v == "" {
			missing = append(missing, f.name)
		}
	}
	if q.From.IsZero() || q.To.IsZero() {
		missing = append(missing, "window")
	}
	if len(missing) > 0 {
		return fmt.Errorf("observability: cycle log query is missing %v", missing)
	}
	if !q.From.Before(q.To) {
		return errors.New("observability: cycle log window ends before it starts")
	}
	return nil
}

func (q CycleLogQuery) scopeName() string {
	if q.Component == "" {
		return "project"
	}
	return "component"
}

func (c *observabilityClient) QueryCycleLogs(ctx context.Context, q CycleLogQuery) ([]LogLine, CycleLogStats, error) {
	if err := q.validate(); err != nil {
		return nil, CycleLogStats{}, err
	}
	r := &cycleLogRead{c: c, q: q, scope: componentScope{
		Namespace: q.Namespace, Project: q.Project, Component: q.Component, Environment: q.Environment,
	}}
	if q.Component == "" {
		r.phrase = runnerLinePhrase
	}
	kept, err := r.run(ctx)
	stats := CycleLogStats{Pages: r.pages, LinesMissing: r.linesMissing}
	if err != nil {
		return nil, stats, err
	}
	out := make([]LogLine, 0, len(kept))
	for _, l := range kept {
		if l.line.ComponentUID == q.ComponentUID {
			out = append(out, l.line)
		}
	}
	return out, stats, nil
}

// cycleLogRead is the state of one paged read.
type cycleLogRead struct {
	c      *observabilityClient
	q      CycleLogQuery
	scope  componentScope
	phrase string
	pages  int
	// linesMissing: the read knows its window holds lines it did not return.
	linesMissing bool
}

func (r *cycleLogRead) run(ctx context.Context) ([]pageLine, error) {
	to := r.q.To.UTC()
	start := r.q.From.UTC()
	// floor: lines before it were kept from an earlier page.
	var floor time.Time
	var kept []pageLine
	for start.Before(to) {
		if r.pages >= maxCycleLogPages {
			slog.WarnContext(ctx, "observer.read_truncated", "componentUid", r.q.ComponentUID, "scope", r.q.scopeName(), "pages", r.pages)
			r.linesMissing = true
			return kept, nil
		}
		page, whole, err := r.page(ctx, start, to, "asc")
		if err != nil {
			return nil, err
		}
		fresh := atOrAfter(page, floor)
		if whole {
			return append(kept, fresh...), nil
		}
		s := page[len(page)-1].at
		if before := beforeSecond(fresh, s); len(before) > 0 {
			kept = append(kept, before...)
			floor, start = s, s.Add(-time.Second)
			continue
		}
		second, err := r.saturatedSecond(ctx, start, to, s, fresh, len(page)-len(fresh))
		if err != nil {
			return nil, err
		}
		kept = append(kept, second...)
		floor, start = s.Add(time.Second), s.Add(time.Second)
	}
	return kept, nil
}

// saturatedSecond completes second s when an ascending page held nothing but s
// past what was already kept. asc is that page's first lines of s; older counts
// the page's lines before s (all of the window before s, since the page ran
// into s). One descending read of the same window cut at s+1s gives the last
// lines of s, and its `total` minus `older` is how many lines s holds.
func (r *cycleLogRead) saturatedSecond(ctx context.Context, start, to, s time.Time, asc []pageLine, older int) ([]pageLine, error) {
	end := s.Add(time.Second)
	if end.After(to) {
		end = to
	}
	desc, whole, total, err := r.pageWithTotal(ctx, start, end, "desc")
	if err != nil {
		return nil, err
	}
	// desc runs newest second first: its lines of s lead. Reversed here so the
	// result runs oldest first; order WITHIN s is not the index's either way
	// (the observer gives no sub-second time to restore it).
	var tail []pageLine
	for i := len(desc) - 1; i >= 0; i-- {
		if desc[i].at.Equal(s) {
			tail = append(tail, desc[i])
		}
	}
	if whole || (len(desc) > 0 && desc[len(desc)-1].at.Before(s)) {
		// The descending read reached past s, so it holds all of s.
		return tail, nil
	}
	joined := joinByIdentity(asc, tail)
	missing := "unknown"
	if total >= 0 {
		n := total - older - len(joined)
		if n <= 0 {
			return joined, nil
		}
		missing = fmt.Sprint(n)
	}
	r.linesMissing = true
	slog.WarnContext(ctx, "observer.read_incomplete", "componentUid", r.q.ComponentUID, "scope", r.q.scopeName(), "second", s, "missing", missing)
	return joined, nil
}

// lineIdentity is the closest thing to a document id the observer returns.
type lineIdentity struct {
	at                        time.Time
	pod, componentUID, logTxt string
}

func identityOf(l pageLine) lineIdentity {
	return lineIdentity{at: l.at, pod: l.line.PodName, componentUID: l.line.ComponentUID, logTxt: l.line.Log}
}

// joinByIdentity is asc followed by the tail lines asc does not already hold.
// Each identity is kept as often as the read that saw it more often saw it.
func joinByIdentity(asc, tail []pageLine) []pageLine {
	inAsc := make(map[lineIdentity]int, len(asc))
	for _, l := range asc {
		inAsc[identityOf(l)]++
	}
	out := append([]pageLine(nil), asc...)
	for _, l := range tail {
		id := identityOf(l)
		if inAsc[id] > 0 {
			inAsc[id]--
			continue
		}
		out = append(out, l)
	}
	return out
}

// page reads one page. whole reports that it holds every line of its window.
func (r *cycleLogRead) page(ctx context.Context, start, end time.Time, order string) ([]pageLine, bool, error) {
	lines, whole, _, err := r.pageWithTotal(ctx, start, end, order)
	return lines, whole, err
}

// pageWithTotal is page plus the window's `total` (-1 when the body has none).
func (r *cycleLogRead) pageWithTotal(ctx context.Context, start, end time.Time, order string) ([]pageLine, bool, int, error) {
	r.pages++
	resp, err := r.c.query(ctx, logsQueryRequest{
		SearchScope:  r.scope,
		StartTime:    start.Format(time.RFC3339),
		EndTime:      end.Format(time.RFC3339),
		Limit:        queryPageLimit,
		SortOrder:    order,
		SearchPhrase: r.phrase,
	})
	if err != nil {
		return nil, false, 0, err
	}
	out := make([]pageLine, 0, len(resp.Logs))
	for _, e := range resp.Logs {
		// The timestamp drives paging: a line without one cannot be placed,
		// and guessing would silently drop or repeat a second.
		at, err := time.Parse(time.RFC3339, e.Timestamp)
		if err != nil {
			return nil, false, 0, fmt.Errorf("observability: entry timestamp %q: %w", e.Timestamp, err)
		}
		at = at.UTC().Truncate(time.Second)
		line := LogLine{Timestamp: at, Log: e.Log}
		if e.Metadata != nil {
			line.ComponentUID, line.PodName = e.Metadata.ComponentUID, e.Metadata.PodName
		}
		out = append(out, pageLine{at: at, line: line})
	}
	total, hasTotal := resp.total()
	whole := len(out) < queryPageLimit || (hasTotal && len(out) >= total)
	if !hasTotal {
		total = -1
	}
	return out, whole, total, nil
}

func atOrAfter(lines []pageLine, floor time.Time) []pageLine {
	if floor.IsZero() {
		return lines
	}
	for i, l := range lines {
		if !l.at.Before(floor) {
			return lines[i:]
		}
	}
	return nil
}

func beforeSecond(lines []pageLine, s time.Time) []pageLine {
	for i, l := range lines {
		if !l.at.Before(s) {
			return lines[:i]
		}
	}
	return lines
}
