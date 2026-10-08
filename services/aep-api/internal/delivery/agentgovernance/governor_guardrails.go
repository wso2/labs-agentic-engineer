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

package agentgovernance

import (
	"context"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/clients/agentmanager"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// GuardrailRecordKey addresses one agent's guardrail record in one environment.
type GuardrailRecordKey struct {
	Org, Project, Component, Environment string
}

// GuardrailStore remembers which policies AEP wrote to an agent's binding —
// the only entries it may later replace or remove — and what became of each
// declared guardrail. Agent Manager's entries carry no owner, so this record is
// the only way to tell AEP's from an operator's.
type GuardrailStore interface {
	// Get returns the names AEP last applied, and whether a record exists.
	Get(ctx context.Context, key GuardrailRecordKey) (owned []string, found bool, err error)
	Put(ctx context.Context, key GuardrailRecordKey, owned []string, outcomes []GuardrailOutcome) error
}

// reconcileGuardrails makes the agent's binding carry the guardrails its spec
// declares, and records what became of each.
//
// NEVER FATAL, by decision: a guardrail that cannot be applied does not stop
// the deploy. The agent ships, the outcome says which guardrail did not land
// and why, and the next deploy tries again. Every failure is therefore
// recorded rather than returned.
//
// OWNERSHIP IS RECORDED BEFORE THE WRITE. A write that lands while its record
// is lost — a failed save, a timeout after Agent Manager applied it — would
// leave AEP's own entry looking like an operator's: a conflict on every later
// deploy, and never removable. So the record first claims everything AEP is
// about to write, alongside what it held before; if that claim cannot be
// saved, nothing is written. A claim on a name that never reached the binding
// is harmless: the next merge drops it.
func (g *Governor) reconcileGuardrails(ctx context.Context, reg registration, in delivery.GovernAgentInput) {
	if g.deps.Guardrails == nil {
		return
	}
	log := slog.With("org", in.OrgID, "project", in.ProjectID, "component", in.Component)
	if in.GuardrailsUnreadable {
		// Unknown is not "declares nothing": stripping every guardrail on a
		// spec that merely failed to read would remove the agent's protection.
		log.WarnContext(ctx, "governance: the agent's spec could not be read; its guardrails are left as they are")
		return
	}
	key := GuardrailRecordKey{Org: in.OrgID, Project: in.ProjectID, Component: in.Component, Environment: in.Environment}
	owned, found, err := g.deps.Guardrails.Get(ctx, key)
	if err != nil {
		log.WarnContext(ctx, "governance: could not read the agent's guardrail record; guardrails left as they are", "error", err)
		return
	}
	if len(in.Guardrails) == 0 && len(owned) == 0 {
		if found {
			// Nothing declared and nothing of AEP's on the binding: clear the
			// outcomes so the Deployments page shows none.
			g.saveGuardrailRecord(ctx, key, nil, nil)
		}
		return
	}

	failAll := func(reason string) {
		outcomes := make([]GuardrailOutcome, 0, len(in.Guardrails))
		for _, d := range in.Guardrails {
			outcomes = append(outcomes, GuardrailOutcome{Policy: d.Policy, Status: GuardrailFailed, Reason: reason})
		}
		g.saveGuardrailRecord(ctx, key, owned, outcomes)
	}

	catalog, err := reg.amp.ListPolicies(ctx, in.OrgID, reg.provider.UUID)
	if err != nil {
		failAll("could not read the AI gateway's guardrail catalog: " + err.Error())
		return
	}
	resolved, outcomes := resolveGuardrails(in.Guardrails, guardrailContext{catalog: catalog, format: reg.format, instructions: in.AgentInstructions,
		toolText: in.AgentToolText, agentUsesTools: in.AgentUsesTools, agentTakesFiles: in.AgentTakesFiles})

	ref := agentmanager.BindingRef{Org: in.OrgID, Project: in.ProjectID, Agent: reg.agentName,
		ConfigID: reg.config.ConfigID, Environment: in.Environment}
	binding, err := reg.amp.ReadBinding(ctx, ref)
	if err != nil {
		failAll("could not read the agent's binding in Agent Manager: " + err.Error())
		return
	}

	next, nowOwned, conflicts, changed := mergeGuardrails(binding.Policies, owned, resolved)
	for i := range outcomes {
		if conflicts[outcomes[i].Policy] {
			outcomes[i] = GuardrailOutcome{Policy: outcomes[i].Policy, Status: GuardrailConflict,
				Reason: "a guardrail of the same policy was added in Agent Manager by hand; it was left as it is"}
		}
	}
	if !changed {
		g.saveGuardrailRecord(ctx, key, nowOwned, outcomes)
		return
	}

	claimed := union(owned, nowOwned)
	if err := g.deps.Guardrails.Put(ctx, key, claimed, outcomes); err != nil {
		log.WarnContext(ctx, "governance: could not record the guardrails about to be written; the binding is left as it is", "error", err)
		return
	}
	if err := reg.amp.WriteBindingPolicies(ctx, ref, binding, next); err != nil {
		for i := range outcomes {
			if outcomes[i].Status == GuardrailApplied || outcomes[i].Status == GuardrailPartial {
				outcomes[i] = GuardrailOutcome{Policy: outcomes[i].Policy, Status: GuardrailFailed,
					Reason: "Agent Manager refused the guardrails: " + err.Error()}
			}
		}
		// The write may still have landed, so the claim stands.
		g.saveGuardrailRecord(ctx, key, claimed, outcomes)
		return
	}
	g.saveGuardrailRecord(ctx, key, nowOwned, outcomes)
}

// saveGuardrailRecord records what AEP owns and the outcomes, and logs every
// outcome that is not a clean apply.
func (g *Governor) saveGuardrailRecord(ctx context.Context, key GuardrailRecordKey, owned []string, outcomes []GuardrailOutcome) {
	log := slog.With("org", key.Org, "project", key.Project, "component", key.Component)
	for _, o := range outcomes {
		if o.Status != GuardrailApplied {
			log.WarnContext(ctx, "governance: a declared guardrail is not fully applied",
				"policy", o.Policy, "status", o.Status, "reason", o.Reason)
		}
	}
	if err := g.deps.Guardrails.Put(ctx, key, owned, outcomes); err != nil {
		log.WarnContext(ctx, "governance: could not record the agent's guardrails", "error", err)
	}
}

// union is a then the names of b not already in a.
func union(a, b []string) []string {
	out := append([]string{}, a...)
	seen := make(map[string]bool, len(a))
	for _, n := range a {
		seen[n] = true
	}
	for _, n := range b {
		if !seen[n] {
			out = append(out, n)
			seen[n] = true
		}
	}
	return out
}
