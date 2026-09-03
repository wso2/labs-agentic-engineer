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

package delivery

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/k8sname"
)

// The merged-pull-request → builds contract, shared by the two halves of the
// milestone loop that must agree on it exactly: the EVENT PLANE triggers the
// fan-out, and the RUN SUPERVISOR reads it back to decide whether the cycle
// landed green. They are peer sub-packages that may not import each other, so
// "which components a merge rebuilds" and "what those runs are called" live
// here — one definition, no drift.

// PathDiff is the outcome of matching a merged pull request's changed files
// against the design's App Paths: the components to build, and the files that
// belong to no component.
type PathDiff struct {
	Components []string
	Unmatched  []string
}

// DiffComponents maps changed files onto components by App Path prefix.
//
// It is generic over who authored the pull request — the merge, not the
// authorship, is what makes a component stale. Unmatched files are returned
// rather than dropped so the caller can WARN about them: a path outside every
// App Path is either a repo-root concern (docs, CI) or a design that has
// drifted from the tree, and silently ignoring the second is how a component
// stops being rebuilt without anybody noticing.
//
// Components are returned in a stable order so a fan-out is reproducible — and
// so the supervisor's expected set is the same list the trigger built.
func DiffComponents(files []string, appPaths map[string]string) PathDiff {
	var out PathDiff
	if len(files) == 0 {
		return out
	}
	claimed := make(map[string]bool, len(files))
	for name, appPath := range appPaths {
		hit := false
		for _, f := range files {
			if fileUnder(f, appPath) {
				claimed[f] = true
				hit = true
			}
		}
		if hit {
			out.Components = append(out.Components, name)
		}
	}
	sort.Strings(out.Components)
	for _, f := range files {
		if !claimed[f] {
			out.Unmatched = append(out.Unmatched, f)
		}
	}
	return out
}

// fileUnder reports whether a changed file lives under a component's App Path.
// An empty App Path means the component builds from the repo root, so every
// change is its change.
func fileUnder(file, appPath string) bool {
	clean := strings.TrimPrefix(strings.TrimSpace(appPath), "./")
	clean = strings.Trim(clean, "/")
	if clean == "" {
		return true
	}
	return file == clean || strings.HasPrefix(file, clean+"/")
}

// ShortSHA is the 12-hex-character form of a commit used in run names, dedupe
// keys and issue prose. Twelve is git's own long-enough-to-be-unique default.
//
// It is NOT what keeps a run name inside its Kubernetes budget — believing that
// is what produced a 64-char name that silently never built. The budget is
// enforced where the name is composed (BuildRunNamePrefix, via k8sname.Bounded),
// so this width is free to serve readability and the GitHub dedupe keys that
// also embed it.
func ShortSHA(sha string) string {
	s := strings.ToLower(strings.TrimSpace(sha))
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// Widths of the readable head of a build run name. The project and component
// are capped because neither has a bounded length — a project carries a
// generated uniqueness suffix, and a component is named by the design agent —
// while the commit is never truncated, because matching a build to a commit is
// the main reason anyone reads one of these names. Both are recoverable in full
// from the run's own `openchoreo.dev/{project,component}` labels, so capping
// them costs nothing but readability.
const (
	runNameProjectWidth   = 18
	runNameComponentWidth = 18
	// maxAttemptDigits is the room reserved for the trailing ordinal. The
	// re-trigger budget allows two attempts per (component, commit); two digits
	// leaves that room an order of magnitude of headroom.
	maxAttemptDigits = 2
)

// buildRunNameBudget is what the prefix may spend: the whole label-value budget
// less the "-<attempt>" that BuildRunName appends. Subtracting the suffix HERE
// is what makes every attempt name bounded by construction.
const buildRunNameBudget = k8sname.MaxLabelValueLen - 1 - maxAttemptDigits

// BuildRunNamePrefix is the (component, commit) half of a build WorkflowRun's
// name — the key both the automatic re-trigger budget and the supervisor's
// cycle-build read count on. Attempts share it and differ only in the trailing
// ordinal, so counting the runs whose name carries this prefix IS the attempt
// count, derived from OpenChoreo rather than stored anywhere.
//
// The name is composed through k8sname.Bounded rather than formatted directly,
// because it must fit MaxLabelValueLen for ANY project and component name: a
// name one character over is accepted by OpenChoreo and then never builds
// (k8sname.MaxLabelValueLen explains why). Bounded truncates the readable head
// to fit and appends a digest of the untruncated identity, so two components
// that share a truncated head still get distinct prefixes and cannot
// contaminate each other's attempt count.
func BuildRunNamePrefix(projectID, component, sha string) string {
	return k8sname.Bounded(buildRunNameBudget,
		k8sname.Capped(projectID, runNameProjectWidth),
		k8sname.Capped(component, runNameComponentWidth),
		k8sname.Whole(ShortSHA(sha)),
	) + "-"
}

// BuildRunName names attempt n (1-based) of a component's build at a commit.
func BuildRunName(projectID, component, sha string, attempt int) string {
	return fmt.Sprintf("%s%d", BuildRunNamePrefix(projectID, component, sha), attempt)
}

// ErrDeployPermanent marks a deploy failure that repeating cannot change: the
// component is gone from the design, or OpenChoreo does not have it.
//
// It exists for the same reason sourcecontrol.IsPermanent does, and splits the
// same way. The supervisor's activities run under Temporal's DEFAULT unbounded
// retry, which is right for a blip and wrong for an ANSWER — a component that no
// longer exists will not start existing on attempt 300, and retrying it hides
// the one failure that mattered behind a thousand copies. WHICH failures are
// permanent belongs to the domain that talks to OpenChoreo; turning that into
// Temporal's vocabulary belongs to run/errors.go. This sentinel is the seam
// between them, so neither has to import the other's world.
var ErrDeployPermanent = errors.New("permanent deploy failure")

// ErrProvisionPermanent marks a provisioning failure that repeating cannot
// change: the ClusterResourceType is missing, or the Resource never cuts a
// release.
//
// It is the same seam as ErrDeployPermanent: WHICH provision failures are
// permanent belongs to the dependencies domain; turning that into Temporal's
// vocabulary belongs to run/errors.go. This sentinel is the seam so run does
// not import dependencies.
var ErrProvisionPermanent = errors.New("permanent provision failure")

// ComponentDeploy is one component's deployment in one environment, as the run
// loop reasons about it: which release was pinned, and what the cluster says
// about the binding that pins it.
//
// It lives here for the same reason the build contract does — the DEPLOY STAGE
// writes it and the supervisor reads it back to decide whether a cycle may
// validate, and a second definition would let the two disagree about what
// "deployed" means.
//
// Ready and Failed are separate booleans rather than a status string because
// the third state — neither, i.e. still rolling out — is the one the poll spends
// most of its time in, and it is the state a status enum keeps tempting callers
// to fold into one of the other two.
type ComponentDeploy struct {
	Component   string `json:"component"`
	Environment string `json:"environment"`
	// Release is the ComponentRelease this deployment pins — the release a
	// promote just wrote, or the one a READ found on the binding. Empty when the
	// binding does not exist yet, and on a converge, which deliberately moves no
	// pin.
	Release string `json:"release,omitempty"`
	// Ready is the binding's aggregate Ready condition being True (or the
	// component being deliberately undeployed).
	Ready bool `json:"ready"`
	// Undeploy is spec.state == Undeploy: somebody took this component out of
	// the environment on purpose. It rides beside Ready, which it also sets,
	// because the two mean opposite things to a RECONCILE — "it is up" versus
	// "nothing is owed here" — and a pass that could not tell them apart would
	// promote a release over a deliberate withdrawal.
	Undeploy bool `json:"undeploy,omitempty"`
	// Failed is Ready=False — a verdict, not a wait.
	Failed bool `json:"failed,omitempty"`
	// Reason is OpenChoreo's own condition reason, carried verbatim for the
	// issue body a failed deployment mints. Never branched on.
	Reason string `json:"reason,omitempty"`
}

// MergeBuild is one component's build at a merge SHA, read back off its
// WorkflowRun. Status and Completed are the run's own pair, carried verbatim:
// OpenChoreo's status is a condition Reason string rather than a closed set, so
// Completed is the terminal gate and Status is display only.
type MergeBuild struct {
	Component string
	RunName   string
	Status    string
	Completed bool
	StartedAt string
	// Attempt is the run name's trailing ordinal: 1 for the fan-out's build, 2
	// for the one automatic re-trigger a red build gets.
	Attempt int
}

// BuildsAtMerge picks the builds belonging to one merge out of a project's
// WorkflowRuns, newest attempt per component, ordered by component name.
//
// This is the READ-side inverse of BuildRunName, and it is a projection of the
// cluster rather than of anything stored — the same rule the re-trigger budget
// follows, for the same reason: a stored copy of per-component build state
// could desynchronise from the cluster, and a run that exists cannot be
// un-counted. Matching is on the name because the name is what carries the
// (component, commit, attempt) triple; the component is read off the run's own
// label rather than parsed back out, so a component whose name contains a dash
// cannot be mis-split.
func BuildsAtMerge(runs []MergeBuild, projectID, sha string) []MergeBuild {
	if sha == "" {
		return nil
	}
	best := map[string]MergeBuild{}
	for _, r := range runs {
		if r.Component == "" {
			continue
		}
		prefix := BuildRunNamePrefix(projectID, r.Component, sha)
		if !strings.HasPrefix(strings.ToLower(r.RunName), prefix) {
			continue
		}
		attempt, err := strconv.Atoi(strings.ToLower(r.RunName)[len(prefix):])
		if err != nil || attempt <= 0 {
			// A name that carries the prefix but no ordinal is not one of ours.
			continue
		}
		r.Attempt = attempt
		if prior, seen := best[r.Component]; !seen || attempt > prior.Attempt {
			best[r.Component] = r
		}
	}
	out := make([]MergeBuild, 0, len(best))
	for _, b := range best {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Component < out[j].Component })
	return out
}
