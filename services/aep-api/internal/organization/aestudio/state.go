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

package aestudio

// state.go — the status state machine (ADR-0045): compare desired with
// live, answer at once, and start a converge on drift. A binding that will
// not become Ready by itself (stuck) answers failed.
//
// The comparison projects each live object onto the keys aep-api writes
// before comparing, so what OpenChoreo adds on its own (schema defaults,
// labels, status, metadata) is never drift. Counting it would answer
// provisioning for ever and PUT on every poll.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

// templateHashAnnotation stamps the installed ResourceType with the hash of
// the template it was written from.
const templateHashAnnotation = "aep.wso2.com/ae-studio-template-hash"

// live is what is installed for an org. A nil object is not installed.
type live struct {
	env string
	// prb is whether the ProjectReleaseBinding (the cell namespace) exists.
	prb      bool
	rt       *openchoreo.ResourceType
	resource *openchoreo.Resource
	binding  *openchoreo.ResourceReleaseBinding
}

// errProjectMissing is the Project ae-system not existing yet.
var errProjectMissing = errors.New("project " + ProjectName + " does not exist")

// Status answers GET /ae-studio for org. It reads through the same clients
// as the converge (aep-api's own identity where configured), starts a
// converge on drift and never waits for one.
// An error is a failure to read, not a state.
func (s *Service) Status(ctx context.Context, org string) (Status, error) {
	st, err := s.status(ctx, org)
	if err == nil && st.State != StateFailed {
		s.statusRecovered(org)
	}
	return st, err
}

// status computes Status's answer.
func (s *Service) status(ctx context.Context, org string) (Status, error) {
	d, err := s.desired(ctx, org)
	var nr *notReadyError
	if errors.As(err, &nr) {
		if nr.state == StateFailed {
			return failed(FailError), nil
		}
		return Status{State: nr.state}, nil
	}
	if err != nil {
		return Status{}, err
	}
	provisioning := Status{State: StateProvisioning, OUID: d.Params.Org.ID}
	if s.busy(org) {
		return provisioning, nil
	}
	if s.recentlyFailed(org, d.fingerprint()) {
		return failed(FailError), nil
	}
	l, err := s.observe(ctx, s.oc, org)
	var noTarget *openchoreo.ErrNoWriteTarget
	switch {
	case errors.As(err, &noTarget):
		level := slog.LevelDebug
		if s.firstStatusFailure(org, d.fingerprint()) {
			level = slog.LevelWarn
		}
		slog.Log(ctx, level, "ae_studio.status_failed", "org", org, "reason", "no write target", "error", err)
		return failed(FailError), nil
	case errors.Is(err, errProjectMissing):
		s.Trigger(ctx, org)
		return provisioning, nil
	case err != nil:
		return Status{}, err
	}
	if reason := drift(d, l); reason != "" {
		slog.InfoContext(ctx, "ae_studio.drift", "org", org, "what", reason)
		s.Trigger(ctx, org)
		return provisioning, nil
	}
	if urls := urlsOf(l.binding); l.binding.IsReady() && urls != nil {
		return Status{State: StateReady, URLs: urls, OUID: d.Params.Org.ID}, nil
	}
	if reason := s.stuck(org, l.binding); reason != "" {
		level := slog.LevelDebug
		if s.firstStatusFailure(org, d.fingerprint()) {
			level = slog.LevelWarn
		}
		slog.Log(ctx, level, "ae_studio.status_failed", "org", org, "reason", reason)
		if reason == reasonPastBound && stillProgressing(l.binding) {
			return failed(FailTimeout), nil
		}
		return failed(FailError), nil
	}
	return provisioning, nil
}

// failed is a failed answer with its reason.
func failed(reason FailReason) Status { return Status{State: StateFailed, Reason: reason} }

// terminalReadyReasons are the Ready=False reasons OC gives a binding whose
// release it cannot render or own, which waiting will not fix. OC never
// reports a data-plane failure (CrashLoopBackOff, ImagePullBackOff, an
// unschedulable pod) as a distinct reason: those stay not Ready and reach
// failed through notReadyBound instead (reason timeout).
var terminalReadyReasons = map[string]bool{
	"RenderingFailed":             true,
	"InvalidReleaseConfiguration": true,
	"ReleaseOwnershipConflict":    true,
}

// reasonPastBound is stuck's reason for a binding not Ready for longer than
// notReadyBound. It answers a timeout, not an error, only while the binding
// is still progressing (stillProgressing): it may still come up by itself.
const reasonPastBound = "not ready past bound"

// progressingReason is the Ready reason OC gives a binding whose workload is
// applied but not ready yet (readyWhen false). It is the same for a pod that
// cannot be scheduled and one whose image will not pull: OC carries no cause.
const progressingReason = "ResourcesProgressing"

// stillProgressing is a binding with no Ready condition yet, or one whose
// Ready reason is progressingReason. Any other not-Ready reason (an apply
// failure, a missing release, environment or data plane) is a fault, not a
// wait.
func stillProgressing(b *openchoreo.ResourceReleaseBinding) bool {
	c := b.ReadyCondition()
	return c == nil || c.Reason == progressingReason
}

// stuck names why a binding that is not Ready will not become so by itself
// ("" while it may): a terminal Ready reason once the last converge has
// settled, or not Ready for longer than notReadyBound (reasonPastBound). The
// reason is an OC reason code or a fixed phrase, never a value.
func (s *Service) stuck(org string, b *openchoreo.ResourceReleaseBinding) string {
	now, converged := s.now(), s.convergedAt(org)
	c := b.ReadyCondition()
	if c != nil && terminalReadyReasons[c.Reason] && now.Sub(converged) >= settleGrace {
		return c.Reason
	}
	since := converged
	if c != nil && c.LastTransitionTime.After(since) {
		since = c.LastTransitionTime
	}
	if !since.IsZero() && now.Sub(since) > notReadyBound {
		return reasonPastBound
	}
	return ""
}

// observe reads what is installed. errProjectMissing: nothing is.
func (s *Service) observe(ctx context.Context, oc OC, org string) (live, error) {
	var l live
	env, err := oc.Targets.Resolve(ctx, org, ProjectName)
	var noTarget *openchoreo.ErrNoWriteTarget
	switch {
	case errors.As(err, &noTarget):
		return l, err
	case errors.Is(err, openchoreo.ErrNotFound):
		return l, errProjectMissing
	case err != nil:
		return l, fmt.Errorf("resolve write target: %w", err)
	}
	l.env = env
	_, err = oc.Cells.ProjectReleaseBindingReadiness(ctx, org, ProjectName, env)
	switch {
	case err == nil:
		l.prb = true
	case !errors.Is(err, openchoreo.ErrNotFound):
		return l, err
	}
	if l.rt, err = oc.Resources.GetResourceType(ctx, org, ResourceName); errors.Is(err, openchoreo.ErrNotFound) {
		l.rt = nil
	} else if err != nil {
		return l, err
	}
	if l.resource, err = oc.Resources.GetResource(ctx, org, ResourceName); errors.Is(err, openchoreo.ErrNotFound) {
		l.resource = nil
	} else if err != nil {
		return l, err
	}
	if l.binding, err = oc.Resources.GetBinding(ctx, org, bindingName(env)); err != nil {
		return l, err
	}
	return l, nil
}

// drift names the first way live differs from d, "" when it does not.
func drift(d desiredState, l live) string {
	switch {
	case !l.prb:
		return "project-binding"
	case rtDrifted(l.rt):
		return "resourcetype"
	case resourceDrifted(d, l.resource):
		return "parameters"
	case l.binding == nil:
		return "binding"
	case bindingDrifted(d, l.binding, openchoreo.ReleaseName(l.resource)):
		return "binding"
	}
	return ""
}

// rtDrifted is a ResourceType not installed, or not from this template.
func rtDrifted(rt *openchoreo.ResourceType) bool {
	return rt == nil || rt.Metadata.Annotations[templateHashAnnotation] != TemplateHash()
}

// resourceDrifted is a Resource not installed, of another type or owner, or
// whose parameters differ from d's on d's keys.
func resourceDrifted(d desiredState, r *openchoreo.Resource) bool {
	if r == nil || r.Spec.Owner.ProjectName != ProjectName || r.Spec.Type.Name != ResourceName ||
		(r.Spec.Type.Kind != "" && r.Spec.Type.Kind != "ResourceType") {
		return true
	}
	want, _ := json.Marshal(d.Params)
	return !sameOnOurKeys(want, r.Spec.Parameters)
}

// bindingDrifted is a binding pinned to another release than latest (or to
// none), or whose environment configs differ from d's on d's keys.
func bindingDrifted(d desiredState, b *openchoreo.ResourceReleaseBinding, latest string) bool {
	if b.Spec.ResourceRelease == "" || b.Spec.ResourceRelease != latest ||
		b.Spec.Owner.ProjectName != ProjectName || b.Spec.Owner.ResourceName != ResourceName {
		return true
	}
	want, _ := json.Marshal(d.EnvConfigs)
	return !sameOnOurKeys(want, b.Spec.ResourceTypeEnvironmentConfigs)
}

// urlsOf reads the three public URLs off a binding's resolved outputs; nil
// until all three are there.
func urlsOf(b *openchoreo.ResourceReleaseBinding) *URLs {
	if b == nil || b.Status == nil {
		return nil
	}
	out := map[string]string{}
	for _, o := range b.Status.Outputs {
		out[o.Name] = o.Value
	}
	u := &URLs{DesignAgent: out["designUrl"], Collab: out["collabUrl"], Tools: out["toolsUrl"]}
	if u.DesignAgent == "" || u.Collab == "" || u.Tools == "" {
		return nil
	}
	return u
}

// bindingName is the ResourceReleaseBinding of the Resource in env.
func bindingName(env string) string { return ResourceName + "-" + env }

// sameOnOurKeys reports whether the live JSON, projected onto the keys of
// want, equals want.
func sameOnOurKeys(want, got json.RawMessage) bool {
	var w, g any
	if err := json.Unmarshal(want, &w); err != nil {
		return false
	}
	if len(got) > 0 {
		if err := json.Unmarshal(got, &g); err != nil {
			return false
		}
	}
	return reflect.DeepEqual(w, projectOnto(g, w))
}

// projectOnto keeps of got only what want has: an object's keys that want
// names, an array's items projected one by one. A key want names and got
// lacks stays absent (so it differs), except an empty array, which an absent
// or null value stands for.
func projectOnto(got, want any) any {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return got
		}
		out := make(map[string]any, len(w))
		for k, wv := range w {
			if gv, ok := g[k]; ok {
				out[k] = projectOnto(gv, wv)
			} else if isEmptyArray(wv) {
				out[k] = []any{}
			}
		}
		return out
	case []any:
		if got == nil && len(w) == 0 {
			return []any{}
		}
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return got
		}
		out := make([]any, len(g))
		for i := range g {
			out[i] = projectOnto(g[i], w[i])
		}
		return out
	default:
		return got
	}
}

func isEmptyArray(v any) bool {
	a, ok := v.([]any)
	return ok && len(a) == 0
}
