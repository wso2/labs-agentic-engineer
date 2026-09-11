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

package gitfs_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs/workspacetest"
)

// TestFetchNeverHandsTheMirrorToAutoMaintenance guards the reaper's charter:
// it is the ONLY thing that may repack a shared mirror, because it is the
// only thing that does so under the per-repo EX flock. Git disagrees by
// default — `fetch` (like `commit`) ends by spawning
// `git maintenance run --auto --no-quiet --detach`, which double-forks and so
// keeps rewriting the object DB after fetch has returned and the engine has
// already released the lock. The spawn is unconditional: the parent hands off
// on every fetch and lets the child decide whether a task has work, so the
// handoff itself is the thing to forbid.
//
// The assertion reads the FETCHING process's own child_start record rather
// than looking for the maintenance process or its packs. Git writes that
// record before it forks, so it is on disk by the time the process we waited
// for has exited: a decision, not a timing window, and nothing to poll.
//
// The control is the teeth, and it runs FIRST. GIT_CONFIG_COUNT=0 makes git
// ignore the engine's env-forced config without the test having to know how many
// entries there are, and the same fetch then has to hand the mirror over —
// proving the trace scan can see a handoff when one happens. Without that, the
// assertion that matters ("no handoff under the engine's env") passes for any
// reason at all, including git never handing off in the first place.
//
// WHICH IS VERSION-DEPENDENT, so the control is a PRECONDITION rather than an
// assertion. Measured: git 2.47.3 (what the aep-api image ships, and the only
// version the guard has to protect) and 2.54.0 both hand a fetch to
// `git maintenance run --auto`; 2.55.0 — which is what `ubuntu-latest` carries —
// does not, and asserting that it must turned this into a red build about git's
// behaviour rather than ours. When the ambient git will not hand off even with
// the forced config disabled, this environment cannot show the handoff the guard
// suppresses, and the test says so instead of passing vacuously.
//
// The forced config itself stays covered everywhere:
// TestForcedConfigOutranksAMirrorAlreadyOnDisk and
// TestGitResolvesEveryForcedConfigRule ask git what it RESOLVES, which is
// deterministic on every version and never skips.
func TestFetchNeverHandsTheMirrorToAutoMaintenance(t *testing.T) {
	fx := workspacetest.New(t, seedFiles())
	ctx := context.Background()

	// Clone the mirror, then drive a real fetch and keep the argv the engine
	// itself used — the replay below runs the engine's fetch, not a hand-typed
	// approximation of it.
	rec := recordCommands(t, fx.Engine)
	mustHead(t, fx, "")
	fx.Origin.Seed(t, map[string]string{"specs/one.md": "one\n"}, "one")
	rec.reset()
	mustHead(t, fx, "")
	fetchArgs := recordedFetchArgv(t, rec)

	// The control, first: with the engine's forced config switched off, this git
	// has to hand the mirror over. If it does not, the guard has nothing
	// observable to suppress here and the assertion below would pass for the
	// wrong reason.
	if !fetchHandsOff(t, fx, ctx, fetchArgs, "control", map[string]string{"GIT_CONFIG_COUNT": "0"}) {
		t.Skipf("%s does not hand a fetch to `git maintenance run --auto` even with the engine's forced config disabled, "+
			"so this environment cannot show the handoff the guard suppresses; the forced config itself is still asserted by "+
			"TestForcedConfigOutranksAMirrorAlreadyOnDisk and TestGitResolvesEveryForcedConfigRule",
			gitVersion(t, fx, ctx))
	}

	if fetchHandsOff(t, fx, ctx, fetchArgs, "engine env", nil) {
		t.Fatal("fetch spawned `git maintenance run --auto` under the engine's env — " +
			"the mirror is being repacked outside the per-repo lock, and the reaper is no longer its only maintainer")
	}
}

// fetchHandsOff replays the engine's own fetch under extra env and reports
// whether the fetching process recorded starting a `git maintenance` child.
//
// It reads the FETCHING process's own child_start record rather than looking for
// the maintenance process or its packs: git writes that record before it forks,
// so it is on disk by the time the process we waited for has exited. A decision,
// not a timing window, and nothing to poll.
func fetchHandsOff(t *testing.T, fx *workspacetest.Fixture, ctx context.Context, fetchArgs []string, label string, extra map[string]string) bool {
	t.Helper()
	// A commit of its own for every replay, so each transfers real objects
	// instead of short-circuiting.
	fx.Origin.Seed(t, map[string]string{"specs/" + label + ".md": "x\n"}, label)

	traceDir := t.TempDir()
	env := map[string]string{"GIT_TRACE2_EVENT": traceDir}
	for k, v := range extra {
		env[k] = v
	}
	if _, err := gitfs.RunGitWithEnv(fx.Engine, ctx, env, fetchArgs...); err != nil {
		t.Fatalf("%s: fetch: %v", label, err)
	}
	if traced := tracedCommands(t, traceDir); !traced["fetch"] {
		t.Fatalf("%s: trace sink captured no fetch — every verdict from it would be vacuous (saw %v)", label, traced)
	}
	return spawnedAutoMaintenance(t, traceDir)
}

// gitVersion reports the `git version` line of the binary the engine runs, for
// a skip message that names the thing it is skipping on.
func gitVersion(t *testing.T, fx *workspacetest.Fixture, ctx context.Context) string {
	t.Helper()
	out, err := gitfs.RunGitWithEnv(fx.Engine, ctx, nil, "version")
	if err != nil {
		return "this git"
	}
	return strings.TrimSpace(string(out))
}

// TestForcedConfigOutranksAMirrorAlreadyOnDisk pins WHERE the knob lives. A
// `git config` stamp written at clone time cannot reach a mirror that was
// cloned before the rule existed, and the shared volume outlives any single
// release. Forcing the value into every child's environment does reach them:
// env-supplied config outranks the repo's own file, so a mirror whose config
// says otherwise still gets the engine's answer.
func TestForcedConfigOutranksAMirrorAlreadyOnDisk(t *testing.T) {
	fx := workspacetest.New(t, seedFiles())
	ctx := context.Background()
	mustHead(t, fx, "")
	gitDir := mirrorGitDir(t, fx)

	// Stand in for a mirror already on the volume: its own config asks for
	// auto maintenance.
	gitOut(t, gitDir, "config", "maintenance.auto", "true")
	if got := gitOut(t, gitDir, "config", "--get", "maintenance.auto"); got != "true" {
		t.Fatalf("setup: mirror config = %q, want true", got)
	}

	out, err := gitfs.RunGitWithEnv(fx.Engine, ctx, nil, "--git-dir", gitDir, "config", "--get", "maintenance.auto")
	if err != nil {
		t.Fatalf("config --get through the engine: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "false" {
		t.Fatalf("git resolved maintenance.auto = %q under the engine env, want false", got)
	}
}

// TestGitResolvesEveryForcedConfigRule asks git itself what it resolves for
// every rule the engine forces, so a rule added to the list is covered the
// moment it is added — and so a rendering mistake (a stale count, a skipped
// index) fails here rather than silently dropping the rule in production.
func TestGitResolvesEveryForcedConfigRule(t *testing.T) {
	rules := gitfs.ForcedConfigRules()
	if len(rules) == 0 {
		t.Fatal("no forced config rules — this test would prove nothing")
	}
	fx := workspacetest.New(t, seedFiles())
	ctx := context.Background()
	mustHead(t, fx, "")
	gitDir := mirrorGitDir(t, fx)

	for _, rule := range rules {
		key, want := rule[0], rule[1]
		out, err := gitfs.RunGitWithEnv(fx.Engine, ctx, nil, "--git-dir", gitDir, "config", "--get", key)
		if err != nil {
			t.Errorf("git config --get %s: %v", key, err)
			continue
		}
		if got := strings.TrimSpace(string(out)); got != want {
			t.Errorf("git resolved %s = %q under the engine env, want %q", key, got, want)
		}
	}
}

// recordedFetchArgv returns the argv of the single fetch the recorder saw,
// minus the leading "git" the hook prepends.
func recordedFetchArgv(t *testing.T, rec *recorder) []string {
	t.Helper()
	var found [][]string
	for _, c := range rec.all() {
		if subcommand(c.Args) == "fetch" {
			found = append(found, c.Args[1:])
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one recorded fetch, got %d", len(found))
	}
	return found[0]
}

// tracedCommands returns the set of git subcommands that wrote a trace file
// into dir (one file per process, named by cmd_name in its events).
func tracedCommands(t *testing.T, dir string) map[string]bool {
	t.Helper()
	seen := map[string]bool{}
	forEachTraceEvent(t, dir, func(e traceEvent) {
		if e.Event == "cmd_name" && e.Name != "" {
			seen[e.Name] = true
		}
	})
	return seen
}

// spawnedAutoMaintenance reports whether any traced process recorded starting
// a `git maintenance` child.
func spawnedAutoMaintenance(t *testing.T, dir string) bool {
	t.Helper()
	found := false
	forEachTraceEvent(t, dir, func(e traceEvent) {
		if e.Event != "child_start" {
			return
		}
		for _, arg := range e.Argv {
			if arg == "maintenance" {
				found = true
			}
		}
	})
	return found
}

// traceEvent is the slice of GIT_TRACE2_EVENT records these assertions read.
type traceEvent struct {
	Event string   `json:"event"`
	Name  string   `json:"name"`
	Argv  []string `json:"argv"`
}

func forEachTraceEvent(t *testing.T, dir string, visit func(traceEvent)) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read trace dir: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read trace file %s: %v", entry.Name(), err)
		}
		for _, line := range strings.Split(string(content), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var e traceEvent
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				t.Fatalf("trace line %q: %v", line, err)
			}
			visit(e)
		}
	}
}
