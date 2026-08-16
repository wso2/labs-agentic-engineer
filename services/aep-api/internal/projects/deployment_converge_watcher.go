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

package projects

import (
	"context"
	"log/slog"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/k8sname"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// defaultConvergeInterval is how often deployed bindings are re-asserted.
//
// Slow on purpose. Every trigger that MATTERS is an event the platform already
// owns — a deploy, a config edit, a design save — so this is the backstop for
// what no event covers, not the mechanism. A tight loop would spend OpenChoreo
// round trips re-writing bindings that are already correct.
const defaultConvergeInterval = 10 * time.Minute

// ProjectLister enumerates the projects the platform tracks. Declared
// consumer-side; the app root satisfies it from the git-repository index, which
// is the one source that sees every project regardless of which rail built it.
type ProjectLister interface {
	ListAll(ctx context.Context) ([]ProjectRef, error)
}

// ProjectRef is one (org, project) pair.
type ProjectRef struct {
	OrgID     string
	ProjectID string
}

// ConvergeWatcher re-asserts every deployed component's ReleaseBinding on a
// ticker.
//
// It replaces two half-backstops that each covered one field of one object: the
// trait drift watcher (dropped when the component_tasks table went away) and the
// env-config.js sweep (which existed because a SPA's own URL resolves only after
// its binding is Ready, so the eager emit could never land it). Both were
// symptoms of the same thing — several writers patching disjoint fields of a
// binding none of them owned. With one writer there is one backstop, and it
// re-asserts the whole object.
//
// What it is FOR is drift no event causes: a hand-edited binding, a write that
// failed transiently after its trigger had passed, an org IDP profile that
// changed under a running deployment. A dropped write here leaves a protected
// API's gateway passing every request through unauthenticated, which is not a
// failure mode worth leaving to chance.
type ConvergeWatcher struct {
	projects  ProjectLister
	deploy    *DeploymentService
	store     *spec.ArtifactStore
	asService func(ctx context.Context) context.Context
	tick      time.Duration
}

// NewConvergeWatcher wires the sweep. tick <= 0 uses the default.
func NewConvergeWatcher(projects ProjectLister, deploy *DeploymentService, store *spec.ArtifactStore,
	asService func(ctx context.Context) context.Context, tick time.Duration) *ConvergeWatcher {
	if tick <= 0 {
		tick = defaultConvergeInterval
	}
	return &ConvergeWatcher{projects: projects, deploy: deploy, store: store, asService: asService, tick: tick}
}

// Run ticks until ctx is cancelled (the app.Watcher shape).
func (w *ConvergeWatcher) Run(ctx context.Context) {
	if w == nil || w.projects == nil || w.deploy == nil {
		return
	}
	ticker := time.NewTicker(w.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.Once(ctx)
		}
	}
}

// Once runs a single sweep. Exported so a test can drive it directly.
//
// One project's failure never stops the others: this is the thing that still has
// to run when something else is broken.
func (w *ConvergeWatcher) Once(ctx context.Context) {
	// The same guard Run applies, repeated because this is an exported entry point:
	// a test or a partially wired root can reach it without ever going through Run.
	if w == nil || w.projects == nil || w.deploy == nil {
		return
	}
	if w.asService != nil {
		ctx = w.asService(ctx)
	}
	refs, err := w.projects.ListAll(ctx)
	if err != nil {
		slog.WarnContext(ctx, "converge watcher: project list failed", "error", err)
		return
	}
	for _, ref := range refs {
		components, cerr := w.componentsOf(ctx, ref)
		if cerr != nil || len(components) == 0 {
			continue
		}
		if err := w.deploy.Converge(ctx, ref.OrgID, ref.ProjectID, components); err != nil {
			slog.WarnContext(ctx, "converge watcher: project did not converge",
				"org", ref.OrgID, "project", ref.ProjectID, "error", err)
		}
	}
}

// componentsOf reads the project's design component names. A project with no
// design yet has nothing to converge and is skipped silently — that is the
// normal state between creating a project and generating its first design.
func (w *ConvergeWatcher) componentsOf(ctx context.Context, ref ProjectRef) ([]string, error) {
	if w.store == nil {
		return nil, nil
	}
	design, err := w.store.ReadDesign(ctx, ref.OrgID, ref.ProjectID)
	if err != nil || design == nil {
		if err != nil && !spec.IsNotFound(err) {
			return nil, err
		}
		return nil, nil
	}
	out := make([]string, 0, len(design.Components))
	for _, c := range design.Components {
		out = append(out, k8sname.ToK8sName(c.Name))
	}
	return out, nil
}
