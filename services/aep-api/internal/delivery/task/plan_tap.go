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

package task

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/taskplan"
)

// planDrainIdleTimeout aborts the upstream drain if no bytes arrive for this
// long. The agents service emits keep-alives ~every 15s, so a longer silence
// means a hung turn — and the drain holds the per-project plan lock (§6, released
// when Stream returns), so without this a hang pins plan_in_progress until a BFF
// restart.
const planDrainIdleTimeout = 90 * time.Second

// planTapMaxLineBytes ceilings one SSE frame. Frames are tool-call JSON, orders
// of magnitude under this; the ceiling exists so a delimiter-less upstream
// cannot grow the read buffer without bound.
const planTapMaxLineBytes = 4 * 1024 * 1024

// planTap streams the agents-service SSE verbatim to the client while parsing
// tool-result frames and performing the GitHub writes for planTask/updateTask
// as they pass (§6). It survives client disconnect: forwarding stops but reading
// continues, so the upstream turn drains to completion and every write lands.
//
// The tap acts on the self-contained tool-RESULT frame (output echoes the
// normalized fields), only when output.ok is true (phase-1 rule).
//
// Every issue it mints is PROSE in a MILESTONE: the milestone number rides the
// create call (so a plan costs 1+N calls, never create-then-patch), the `aep`
// label marks it as the run's working set, and nothing in the body is ever
// parsed back. Re-plan reconcile is therefore ADDITIVE-ONLY, deduped on the
// title slug against the milestone's existing issues plus this run's creations —
// which is also what makes a crash re-run land no duplicates.
type planTap struct {
	ctx       context.Context // detached — survives client disconnect (drain, §6)
	orgID     string
	projectID string
	issues    IssueClient
	// writer mints the Task issues. The planner's own updateTask edits (title,
	// body, the write-failure comment) stay on `issues`: those are the agent's
	// tool surface acting on an issue that already exists, not platform mints,
	// and they carry none of the label or dedupe policy the writer owns.
	writer *delivery.IssueWriter

	// milestone is the version's milestone NUMBER, assigned at issue creation.
	// Zero leaves creations unassigned (no milestone was minted for this turn).
	milestone int
	// appPaths maps a design component name (lowercased) to its source directory,
	// componentStories maps a component id (lowercased) to its IN-SCOPE story
	// citations (#369) — the source of the platform-stamped Serves-stories
	// block. Nil on a scope-less legacy plan.
	componentStories map[string][]int
	// rendered into the body as the App Path the agent works in. Empty when no
	// design reader is wired.
	appPaths map[string]string

	// state carries BOTH pre-existing Tasks (preloaded by the plan assembler,
	// addressable by updateTask{issueNumber}) and this-run creations.
	state map[int]plannedTask

	// contextNumbers is the frozen set of issue numbers preloaded into the turn's
	// context. An updateTask{issueNumber} ref MUST target a member: the agent only
	// ever sees the numbers the assembler pushed, so an out-of-context number is a
	// hallucination (or a human bug report that happens to share the id space) and
	// must NEVER be written — doing so would clobber an unrelated issue's body.
	contextNumbers map[int]bool

	titleToNumber map[string]int // normalized this-run title → created issue number
	// existingSlugs is the titleSlug of every issue already in the milestone;
	// createdSlugs the ones minted this run. Together they are the whole dedupe
	// primitive — there is no machine-block key to compare any more.
	existingSlugs map[string]bool
	createdSlugs  map[string]bool
	// componentToNumber resolves a "Depends on" component name to the issue this
	// run planned for it, so dependency lines carry real issue numbers.
	componentToNumber map[string]int

	// idleTimeout overrides planDrainIdleTimeout (tests set a small value). Zero
	// uses the default.
	idleTimeout time.Duration

	failures int
}

// newPlanTap builds a tap with every map initialised. Callers set the milestone,
// the preloaded state and the app paths.
// storiesFor resolves a component's in-scope story citations for the stamp;
// nil when the scope carries none for it.
func (t *planTap) storiesFor(component string) []int {
	return t.componentStories[strings.ToLower(strings.TrimSpace(component))]
}

func newPlanTap(ctx context.Context, orgID, projectID string, issues IssueClient, writer *delivery.IssueWriter) *planTap {
	return &planTap{
		ctx:               ctx,
		orgID:             orgID,
		projectID:         projectID,
		issues:            issues,
		writer:            writer,
		state:             map[int]plannedTask{},
		contextNumbers:    map[int]bool{},
		titleToNumber:     map[string]int{},
		existingSlugs:     map[string]bool{},
		createdSlugs:      map[string]bool{},
		componentToNumber: map[string]int{},
	}
}

// scanCompleteLines yields only NEWLINE-TERMINATED lines, delimiter included.
//
// Both halves matter here. Keeping the delimiter is what lets the tap forward
// the upstream's bytes verbatim — the client is reading SSE, where the newline
// is the framing, and bufio.ScanLines would strip it (and any \r with it).
// Refusing the undelimited remainder at EOF is what stops a severed upstream
// from putting a half-written `data: {…` frame on the wire: at that point the
// bytes are consumed and dropped, not emitted.
func scanCompleteLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i+1], nil
	}
	if atEOF {
		// Consume the partial so Scan terminates instead of spinning on it.
		return len(data), nil, nil
	}
	return 0, nil, nil // need more bytes to complete the line
}

// streamPartFrame is the minimal shape of an agents-service SSE frame the tap
// reads: the raw StreamPart (type + the self-contained tool output).
type streamPartFrame struct {
	Type   string          `json:"type"`
	Output json.RawMessage `json:"output"`
}

// Stream forwards the upstream body to w verbatim while tapping tool frames —
// each line is consumed (GitHub write) before it is forwarded, so a delivered
// ok tool-result implies the corresponding issue write already landed. It
// closes body on return. Forwarding stops on the first client write error; the
// read loop continues so the upstream drains and all GitHub writes land (§6). An
// idle-read watchdog aborts the drain (closing body) if the upstream goes silent
// past the idle deadline, so a hung turn can't pin the per-project plan lock.
func (t *planTap) Stream(body io.ReadCloser, w io.Writer, flush func()) {
	defer body.Close()

	idle := t.idleTimeout
	if idle <= 0 {
		idle = planDrainIdleTimeout
	}
	// Watchdog: reset on every read; on expiry close body to unblock the pending
	// read and end the drain. atomic flag distinguishes an idle-abort from a
	// clean EOF so it surfaces in the write-failure accounting.
	var idleAborted atomic.Bool
	activity := make(chan struct{}, 1)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		timer := time.NewTimer(idle)
		defer timer.Stop()
		for {
			select {
			case <-stop:
				return
			case <-activity:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(idle)
			case <-timer.C:
				idleAborted.Store(true)
				_ = body.Close()
				return
			}
		}
	}()

	// Bound the per-line read. bufio.Reader.ReadBytes grows until it finds the
	// delimiter, so an upstream that never sends one — a wedged agents-service, a
	// corrupted stream — would grow it until the BFF dies. Scanner takes an
	// explicit ceiling and reports hitting it. (Siblings: agent_progress.go 1MiB,
	// usage_capture.go 16MiB.)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 8*1024), planTapMaxLineBytes)
	scanner.Split(scanCompleteLines)
	clientAlive := true
	for scanner.Scan() {
		line := scanner.Bytes()
		// Reset the idle watchdog on any read return (bytes or a keep-alive line).
		select {
		case activity <- struct{}{}:
		default:
		}
		// Consume BEFORE forwarding: an ok tool-result frame reaching the
		// client means its GitHub write already landed, so the FE can
		// refresh its task list on that frame and see the issue (§8).
		t.consume(line)
		if clientAlive {
			if _, werr := w.Write(line); werr != nil {
				clientAlive = false
			} else {
				flush()
			}
		}
	}
	// Only a delimited line is a whole frame. ReadBytes used to hand back the
	// trailing partial together with its error, and the loop forwarded it before
	// noticing — so severing the upstream mid-frame (which the idle watchdog
	// below does on purpose) wrote a half-written `data: {…` to the client. The
	// console drops unparseable frames, so this was survivable, but a proxy
	// should not emit a frame it did not finish reading.
	//
	// A scan error also ENDS the drain, where the old unbounded ReadBytes would
	// have kept going — so it is counted like the idle abort below rather than
	// only logged. Anything past the offending frame is unread, which means
	// GitHub writes the turn intended may not have landed, and the terminal
	// surface has to say so.
	if err := scanner.Err(); err != nil {
		t.failures++
		slog.WarnContext(t.ctx, "task.planTap: upstream stream ended mid-frame — drain stopped, remaining frames unread",
			"error", err, "maxLineBytes", planTapMaxLineBytes)
	}
	if idleAborted.Load() {
		// Record the aborted drain so the terminal surface reports it; the plan
		// lock releases as PlanSession.Stream returns (its defer).
		t.failures++
		slog.WarnContext(t.ctx, "plan tap: upstream idle past deadline — drain aborted, plan lock released", "idleTimeout", idle)
	}
	// Terminal in-band surface of mid-stream write failures (§6, the OPEN item):
	// an SSE comment line (ignored by StreamPart readers) so the failure count is
	// visible in the raw stream / logs without corrupting the frame protocol.
	if t.failures > 0 && clientAlive {
		_, _ = fmt.Fprintf(w, ": aep-plan-write-failures %d\n\n", t.failures)
		flush()
	}
}

// consume parses one SSE line and, if it is a successful task tool-result,
// performs the corresponding GitHub write.
func (t *planTap) consume(line []byte) {
	data, ok := strings.CutPrefix(strings.TrimSpace(string(line)), "data:")
	if !ok {
		return
	}
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" {
		return
	}
	var frame streamPartFrame
	if err := json.Unmarshal([]byte(data), &frame); err != nil {
		return // partial / non-JSON / keep-alive
	}
	if frame.Type != "tool-result" || len(frame.Output) == 0 {
		return
	}
	ok, op, err := taskplan.ToolResultOK(frame.Output)
	if err != nil || !ok {
		return // skip ok:false and non-task results (self-correction, other tools)
	}
	switch op {
	case "plan":
		if out, derr := taskplan.DecodePlanTaskOk(frame.Output); derr == nil {
			t.handlePlan(out)
		}
	case "update":
		if out, derr := taskplan.DecodeUpdateTaskOk(frame.Output); derr == nil {
			t.handleUpdate(out)
		}
	}
}

// handlePlan mints one Task issue into the version's milestone for a planTask
// result. Dedupe is the titleSlug of the title against the milestone's existing
// issues and this run's creations — so a re-plan is additive-only and a crash
// re-run converges to no-ops.
func (t *planTap) handlePlan(out *taskplan.PlanTaskOk) {
	slug := titleSlug(out.Title)
	norm := normalizeTitle(out.Title)
	if slug != "" && (t.existingSlugs[slug] || t.createdSlugs[slug]) {
		return
	}
	if _, dup := t.titleToNumber[norm]; dup {
		return
	}
	planned := plannedTask{
		Component: out.Component,
		AppPath:   t.appPathFor(out.Component),
		DependsOn: out.DependsOn,
		Rationale: out.Rationale,
	}
	// Milestone assignment RIDES the create — one call, which is what keeps the
	// plan's API cost at 1+N rather than 1+2N. No DedupeKey, deliberately: this
	// is the ONE mint in the domain that dedupes client-side, on the title slug
	// above, because a re-plan legitimately re-proposes a Task under a title the
	// agent may have reworded and the milestone's own membership is the set to
	// reconcile against.
	number, _, err := t.writer.Mint(t.ctx, t.orgID, t.projectID, delivery.IssueSpec{
		Title: out.Title,
		// The Serves-stories stamp is platform-authored from the design's
		// citations (#369) — the planner has zero discretion over it.
		Body: delivery.StampServesStories(composeTaskBody(planned, t.issueForComponent), t.storiesFor(out.Component)),
		// Armed, and PLANNED work: a Task is what the spec asked for. The kind is
		// what keeps a bug-fix run off it — that loop works the deployed version
		// and must never pick up planned work for a version still being built.
		// Gates and the validation task are minted elsewhere and are deliberately
		// not this population.
		Labels:    []string{delivery.LabelAgentWork, delivery.KindDevelopment},
		Milestone: t.milestone,
	})
	if err != nil || number == 0 {
		// No issue exists to flag — record for the terminal in-band surface.
		//
		// A zero number counts as a failure for the same reason an error does:
		// nothing was filed that a later `updateTask` could address. Recording it
		// would put issue 0 in the slug and component maps, which is worse than
		// recording nothing — the next Task naming the same component would then
		// render a "Depends on #0" line into an agent's prose.
		t.failures++
		slog.WarnContext(t.ctx, "plan tap: create issue filed nothing", "title", out.Title, "error", err)
		return
	}
	if slug != "" {
		t.createdSlugs[slug] = true
	}
	t.titleToNumber[norm] = number
	if c := strings.ToLower(strings.TrimSpace(out.Component)); c != "" {
		t.componentToNumber[c] = number
	}
	t.state[number] = planned
}

// appPathFor resolves a component's source directory from the design, or ""
// when the design pins none (the component builds from the repo root) or no
// design reader is wired.
func (t *planTap) appPathFor(component string) string {
	if len(t.appPaths) == 0 {
		return ""
	}
	return t.appPaths[strings.ToLower(strings.TrimSpace(component))]
}

// issueForComponent resolves a dependency's component name to the issue this
// run planned for it. A forward reference (the dependency's own Task has not
// been minted yet) misses, and the body names the component instead.
func (t *planTap) issueForComponent(component string) (int, bool) {
	n, ok := t.componentToNumber[strings.ToLower(strings.TrimSpace(component))]
	return n, ok
}

// handleUpdate patches an existing or this-run Task for an updateTask result.
func (t *planTap) handleUpdate(out *taskplan.UpdateTaskOk) {
	number, ok := t.resolveRef(out.Ref)
	if !ok {
		// An out-of-context {issueNumber} is a real hazard, not a benign miss: it
		// would clobber an unrelated issue. Skip the op with NO GitHub write and
		// record it in the write-failure accounting (same surface as other tap
		// failures). A {title} miss is a benign skip (unknown this-run title).
		if out.Ref.IssueNumber != nil {
			t.failures++
			slog.WarnContext(t.ctx, "plan tap: updateTask ref out of context — skipped (would clobber an unrelated issue)", "issue", *out.Ref.IssueNumber)
		}
		return
	}
	st := t.state[number] // zero value is a benign empty state for a pre-existing miss
	prior := delivery.ParseServesStories(st.Body)

	set := out.Set
	if set.Title != nil && strings.TrimSpace(*set.Title) != "" {
		if err := t.issues.EditIssueTitle(t.ctx, t.orgID, t.projectID, number, *set.Title); err != nil {
			t.recordFlag(number, err)
		}
		// Remap the this-run title index: the echoed ref.title is the canonical
		// pre-rename title (phase-1); future {title} refs use the new title.
		if out.Ref.Title != nil {
			delete(t.titleToNumber, normalizeTitle(*out.Ref.Title))
		}
		t.titleToNumber[normalizeTitle(*set.Title)] = number
		if s := titleSlug(*set.Title); s != "" {
			t.createdSlugs[s] = true
		}
	}
	if set.DependsOn != nil {
		st.DependsOn = *set.DependsOn
	}
	if set.Rationale != nil {
		st.Rationale = *set.Rationale
	}
	if set.Body != nil {
		st.Body = *set.Body
	}
	// The whole body is re-rendered from the current facts, so a patch never
	// accumulates: the dependency lines are re-resolved too, which is how a
	// forward reference planned before its dependency picks up the real issue
	// number once updateTask touches it. The Serves-stories stamp is restamped
	// from the design's citations, falling back to the stamp the body carried
	// (a pre-existing task whose component the scope no longer names must not
	// lose its lineage).
	stories := t.storiesFor(st.Component)
	if len(stories) == 0 {
		stories = prior
	}
	body := delivery.StampServesStories(composeTaskBody(st, t.issueForComponent), stories)
	if err := t.issues.EditIssueBody(t.ctx, t.orgID, t.projectID, number, body); err != nil {
		t.recordFlag(number, err)
	}
	t.state[number] = st
}

// resolveRef resolves an updateTask ref to an issue number: {issueNumber} for a
// pre-existing Task, {title} for one planned earlier this run (§9.3). The agent
// never sees issue numbers it did not receive, so an {issueNumber} that is not a
// member of the preloaded context set is rejected (ok=false) — the tenant-scoped
// fence against clobbering an unrelated issue — and a {title} miss is skipped.
func (t *planTap) resolveRef(ref taskplan.TaskRef) (int, bool) {
	if ref.IssueNumber != nil {
		n := *ref.IssueNumber
		return n, t.contextNumbers[n]
	}
	if ref.Title != nil {
		n, ok := t.titleToNumber[normalizeTitle(*ref.Title)]
		return n, ok
	}
	return 0, false
}

// recordFlag records a mid-stream write failure and says so on the affected
// issue, so a human reading the milestone can see that this Task's brief is
// incomplete. The count also reaches the caller: a plan whose writes did not
// all land settles the run it was filling rather than supervising a short
// milestone.
func (t *planTap) recordFlag(number int, err error) {
	t.failures++
	slog.WarnContext(t.ctx, "plan tap: update issue failed", "issue", number, "error", err)
	if cerr := t.issues.CommentIssue(t.ctx, t.orgID, t.projectID, number,
		"⚠️ A plan update to this Task failed to apply. Re-run the build's planning pass, or edit it by hand."); cerr != nil {
		slog.WarnContext(t.ctx, "plan tap: write-failure comment failed", "issue", number, "error", cerr)
	}
}

// normalizeTitle matches the agents-service title normalization (trim + lower)
// so this side's title→issue map keys identically to the echoed refs (§9.3).
func normalizeTitle(title string) string {
	return strings.ToLower(strings.TrimSpace(title))
}
