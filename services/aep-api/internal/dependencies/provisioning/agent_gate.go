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

// agent_gate.go — the build-time Agent Manager gate.
//
// A version whose design declares at least one `ai-agent` gets one `provision`
// gate, titled "Register agents with Agent Manager", alongside the
// per-dependency gates. The ensure that resolves it runs synchronously in this
// same call, so the gate is minted, worked and closed in one pass on the happy
// path — the same shape as the roles gate (roles_gate.go), for the same reasons.
//
// **ONE GATE PER VERSION, listing every agent.** Not one per agent: an agent's
// registration cannot fail on its own here, because the expensive, fallible
// half — the credential — is not done at this stage at all. A per-agent gate
// would buy independent open/closed states that nothing can produce.
//
// **Why its own gate, rather than a dependency gate.** Model access is granted
// by component TYPE, not declared as a dependency (ADR-0016): every `ai-agent`
// gets it, and there is nothing in a design to name. So this rides the
// `aep:gate/` prefix like roles, driven by the DESIGN at the tag rather than by
// the build drawer's inputs — which also means it runs on a rebuild whose
// dependencies are all already Ready and therefore carry no input.
//
// **What it does NOT do: the key.** The credential is settled by the deploy
// (agentgovernance.Governor.GovernAgent). The key reconcile has a rotate branch
// — the only route back to a known state when Agent Manager holds a key AEP
// cannot read — and rotation is safe only because a rollout is in flight to
// carry the new value. Here there is none: a whole coding cycle stands between
// this gate and the next deploy, so a rotation would cut off the agent that is
// currently RUNNING for all of it, and permanently if the run then failed.
//
// **What failure does.** Nothing a coding agent writes depends on these records
// existing — an agent reads its model access from env vars at runtime, not from
// AMP. The gate earns its keep when the ensure FAILS: if Agent Manager is down,
// or the org has connected no Anthropic key, a full coding → build → deploy →
// validate cycle would end with an agent that cannot reach a model, and the
// only symptom would be a 503 from its own /healthz.
//
// So a failure here is reported as a ProvisionFailure and the caller collapses
// the failure list into one error (`app/build_adapters.go`): the run SETTLES as
// failed at planning rather than waiting on a hold. That is stronger than a
// dispatch hold, not weaker — nothing downstream starts. The gate issue is left
// OPEN as the durable record of why, carrying the error in its body, and the
// next build re-runs the ensure — it is idempotent — and closes it.
//
// Registration is also re-asserted on every deploy and by the converge sweep,
// so this gate is the FIRST assertion rather than the only one. Drift a human
// causes in the AMP console — deleting a binding on the screen they attach
// guardrails from — still self-heals within a converge tick.

package provisioning

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// agentGate names this gate. It rides the `aep:gate/` prefix, not `aep:dep/`,
// so it can never be confused with a gate for a design dependency.
const agentGate = "agent-manager"

// agentGateTitle is the gate's issue title.
const agentGateTitle = "Register agents with Agent Manager"

// ensureAgentGate mints the Agent Manager gate and resolves it.
//
// Order: ASK, MINT, WORK, CLOSE — the same as the roles gate, minus its
// PUBLISH step, because nothing secret is produced here.
//
// A read failure at the ASK step is a FAILURE, never "this version has no
// agents". Conflating the two would let a build register nothing and file no
// ticket while reporting success.
func (s *Service) ensureAgentGate(ctx context.Context, orgID, projectID, tag string, milestoneNumber int) *ProvisionFailure {
	if s.agents == nil || !s.agents.Enabled() {
		return nil
	}

	components, err := s.agents.GovernedAgents(ctx, orgID, projectID)
	if err != nil {
		slog.ErrorContext(ctx, "provisioning: could not read the design to find this version's agents",
			"project", projectID, "tag", tag, "error", err)
		return &ProvisionFailure{Dependency: agentGate, Reason: err.Error()}
	}
	if len(components) == 0 {
		// The design carries no ai-agent component — nothing to register, and no
		// gate to mint. This is the ONLY silent path.
		return nil
	}

	number, minted := s.mintAgentGate(ctx, orgID, projectID, tag, milestoneNumber)

	outcome, err := s.agents.RegisterAgentsForBuild(ctx, orgID, projectID, components)
	if err != nil {
		slog.ErrorContext(ctx, "provisioning: agent registration failed — gate left open, run will settle",
			"project", projectID, "tag", tag, "gate", number, "error", err)
		if minted && number > 0 {
			s.commentAgentGateFailure(ctx, orgID, projectID, number, err)
		}
		return &ProvisionFailure{Dependency: agentGate, Reason: err.Error()}
	}

	if minted && number > 0 {
		if cerr := s.issues.CloseIssue(ctx, orgID, projectID, number,
			agentGateClosingComment(outcome)); cerr != nil {
			// The records exist; only the close failed. Leaving the gate open
			// holds the next dispatch, and the next build's ensure — idempotent
			// — closes it. Failing the build here would fail one whose agents
			// are already registered.
			slog.WarnContext(ctx, "provisioning: close agent gate failed",
				"project", projectID, "gate", number, "error", cerr)
		}
	}
	return nil
}

// existingAgentGate finds this version's agent gate in ANY state, or 0.
//
// ANY state is the point: an OPEN one would have been caught by the DedupeKey,
// and the case that needs catching is the one an earlier attempt of the same
// activity filed and then closed. A read failure answers 0 and the caller
// mints — a duplicate ticket is noise a human can close, and unlike the roles
// gate there is no secret being republished.
func (s *Service) existingAgentGate(ctx context.Context, orgID, projectID, tag string) int {
	want := PlatformGateLabelPrefix + depSlug(agentGate)
	issues, err := s.issues.ListIssues(ctx, orgID, projectID, []string{delivery.KindProvision})
	if err != nil {
		slog.WarnContext(ctx, "provisioning: list issues for the agent gate failed — minting",
			"project", projectID, "tag", tag, "error", err)
		return 0
	}
	for _, issue := range issues {
		if !gateIsForVersion(issue.Labels, tag) {
			continue
		}
		if delivery.HasLabel(issue.Labels, want) {
			return issue.Number
		}
	}
	return 0
}

// mintAgentGate creates the gate issue, deduped per (project, tag).
//
// A create failure is logged and reported as not-minted, and the ensure still
// runs: unlike the roles gate this ticket carries nothing the platform cannot
// reproduce, so a missing audit issue must not stop a version's agents being
// registered.
func (s *Service) mintAgentGate(ctx context.Context, orgID, projectID, tag string, milestoneNumber int) (int, bool) {
	if n := s.existingAgentGate(ctx, orgID, projectID, tag); n > 0 {
		return n, true
	}
	req := sourcecontrol.CreateIssueRequest{
		Title:  agentGateTitle,
		Body:   agentGatePendingBody(),
		Labels: withGateVersion(platformGateLabels(agentGate), tag),
		// Same shape as the roles gate's key: one per version, idempotent across
		// a crashed re-run.
		DedupeKey: "gate:" + projectID + ":" + tag + ":" + agentGate,
	}
	if milestoneNumber > 0 {
		n := milestoneNumber
		req.Milestone = &n
	}
	res, err := s.issues.CreateIssue(ctx, orgID, projectID, req)
	if err != nil {
		slog.WarnContext(ctx, "provisioning: create agent gate failed",
			"project", projectID, "tag", tag, "error", err)
		return 0, false
	}
	if res == nil {
		return 0, false
	}
	// Re-assert the labels: CreateIssue's label pre-creation is best-effort and
	// GitHub SILENTLY DROPS a label that does not exist yet. A `provision` label
	// that never landed is a gate that holds nothing — the dispatch predicate
	// counts by label. AddLabels is idempotent, so paying for it every mint is
	// cheaper than that class of silent failure.
	if lerr := s.issues.AddLabels(ctx, orgID, projectID, res.Number,
		withGateVersion(platformGateLabels(agentGate), tag)); lerr != nil {
		slog.WarnContext(ctx, "provisioning: could not re-assert the agent gate labels — the dispatch hold is BY LABEL",
			"project", projectID, "gate", res.Number, "error", lerr)
	}
	return res.Number, true
}

// commentAgentGateFailure records why the ensure could not finish, on the gate
// that is now holding the run.
func (s *Service) commentAgentGateFailure(ctx context.Context, orgID, projectID string, number int, cause error) {
	body := "**Could not register this version's agents with Agent Manager, so this build " +
		"failed at planning.**\n\nNothing was dispatched: an agent whose model access is " +
		"governed reaches its model through Agent Manager, so building one before these " +
		"records exist would end in an agent that answers 503 from its own `/healthz` and " +
		"a validation verdict that means nothing.\n\nThis gate stays open as the record; the " +
		"next build re-runs the same step and closes it.\n\n" +
		fmt.Sprintf("```\n%s\n```\n", cause.Error())
	if err := s.issues.CommentIssue(ctx, orgID, projectID, number, body); err != nil {
		slog.WarnContext(ctx, "provisioning: comment agent gate failure",
			"project", projectID, "gate", number, "error", err)
	}
}

// agentGatePendingBody is the gate's prose when it is minted, before the work
// runs — like every other gate, it says what is about to happen.
func agentGatePendingBody() string {
	return "Each `ai-agent` component in this version is registered with WSO2 Agent Manager as an " +
		"externally-hosted agent, and bound to this organisation's LLM provider, so its model " +
		"traffic leaves through the AI gateway where guardrails run.\n\n" +
		"The platform resolves this gate itself — no agent works it. The organisation's LLM " +
		"provider is shared: one is created the first time any agent in this organisation is " +
		"governed, and reused afterwards with its credential re-asserted.\n\n" +
		"Each agent's own model credential is issued later, by the deploy that carries it into " +
		"the running pod — not here, because it can only be read once.\n\n" +
		"When this gate closes it lists every agent's model endpoint, which is where a guardrail " +
		"is attached in the Agent Manager console."
}

// agentGateClosingComment is what the gate closes with: exactly what the ensure
// did, so the milestone carries a durable record — and the proxy addresses a
// human needs in order to attach a guardrail.
func agentGateClosingComment(out AgentRegistrationOutcome) string {
	var b strings.Builder
	b.WriteString("Agents registered with Agent Manager.\n\n")
	if out.Provider != "" {
		fmt.Fprintf(&b, "Bound to the organisation's LLM provider `%s`.\n\n", out.Provider)
	}
	for _, a := range out.Agents {
		if a.Skipped {
			// Deliberately not governed is a fact worth stating, not silence: a
			// reader has to be able to tell "governed" from "nothing happened".
			fmt.Fprintf(&b, "- `%s` — not governed (%s); it runs on the organisation's own key.\n",
				a.Component, a.Reason)
			continue
		}
		fmt.Fprintf(&b, "- `%s` — registered and bound. Model endpoint: `%s`\n", a.Component, a.ProxyURL)
	}
	b.WriteString("\nAttach a guardrail to an agent from its page in the Agent Manager console; " +
		"it takes effect on that agent's next call, with no redeploy. " +
		"Nothing the platform writes afterwards removes it.\n")
	return b.String()
}
