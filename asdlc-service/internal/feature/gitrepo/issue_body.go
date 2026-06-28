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

package gitrepo

import (
	"fmt"
	"strings"

	"github.com/wso2/asdlc/asdlc-service/models"
)

// IssueTitle returns the GitHub issue title for a ComponentTask.
func IssueTitle(task *models.ComponentTask) string {
	return task.Title
}

// BuildIssueBody produces the markdown body for the GitHub issue that anchors
// a single ComponentTask. The body is intentionally task-specific — workflow,
// constraints, deny-list, and project-structure conventions live in the
// `asdlc` skill (`remote-worker/plugin/skills/asdlc/SKILL.md`), which the
// platform's coding-agent loads at dispatch and a local-flow developer
// installs into Claude Code via `claude plugin install`.
//
// Layout (top → bottom):
//
//  1. Rationale blockquote (LLM-authored, one short sentence).
//  2. LLM-authored body — 5 ## sections (Overview / Scope / Acceptance
//     criteria / References / Task dependencies). Streamed by the
//     tech-lead detail phase.
//  3. Component Reference card (name, type, language, app path,
//     OpenAPI pointer).
//
// Sibling URLs (and OIDC config for SPAs with
// `callerIdentity.mode: end-user`) reach the SPA at request time via
// `window._env_`, populated by an `env-config.js` file the BFF mounts
// into the SPA's pod via its `ReleaseBinding`. Neither the issue body
// nor the agent ever hand-copies a URL. CORS is handled at the gateway
// via the ClusterComponentType's Envoy filter — backends MUST NOT ship
// CORS middleware. The empty-URL silent-fallback bug (a `?? ""` default
// that turned every fetch into a relative URL and produced `405 Method
// Not Allowed` from the SPA's own nginx) is guarded against by:
//  1. SKILL.md mandating `throw` on missing key in `src/env.ts` (no
//     silent same-origin fallback);
//  2. tech-lead's per-task issue body Setup subsection enumerating
//     each upstream as `window._env_.<UPSTREAM>_URL`; and
//  3. architect validator rule that every web-app's `dependsOn` is
//     matched in `componentAgentInstructions` with the upstream's
//     expected `window._env_` key.
//
// All of this lives in the `asdlc` skill and the architect/tech-lead
// prompts; the issue body remains task-specific.
//
// The agent owns branch + PR creation. The PR body MUST contain
// `Closes #<this-issue>` so the platform's pull_request webhook can link
// the PR back to this task — we restate that as a single trailing line
// here for the human-reading-on-GitHub audience and as a fail-safe in
// case the skill is somehow not loaded.
//
// The two unused parameters (repoURL, repoSlug) are kept to preserve the
// existing call sites. Drop them once the call sites stop passing them.
func BuildIssueBody(task *models.ComponentTask, comp *models.DesignComponent, _repoURL, _repoSlug string) string {
	var sb strings.Builder

	if task.Rationale != "" {
		sb.WriteString("> ")
		sb.WriteString(task.Rationale)
		sb.WriteString("\n\n")
	}

	if task.Body != "" {
		sb.WriteString(task.Body)
		if !strings.HasSuffix(task.Body, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("---\n\n")

	// Component reference card — points the agent at the multi-file design
	// tree under `specs/design/` rather than inlining contracts/specs.
	sb.WriteString("## Component Reference\n")
	sb.WriteString(fmt.Sprintf("- **Name:** %s\n", task.ComponentName))
	if comp != nil {
		sb.WriteString(fmt.Sprintf("- **Type:** %s\n", comp.ComponentType))
		sb.WriteString(fmt.Sprintf("- **Language/Stack:** %s\n", comp.Language))
		if comp.AppPath != "" {
			sb.WriteString(fmt.Sprintf("- **App Path (within repo):** `%s`\n", normalizeAppPath(comp.AppPath)))
		}
		sb.WriteString(fmt.Sprintf("- **Design:** `specs/design/components/%s/design.md`\n", task.ComponentName))
		if comp.OpenAPISpec != "" {
			sb.WriteString(fmt.Sprintf("- **Contract:** `specs/design/components/%s/openapi.yaml`\n", task.ComponentName))
		}
		sb.WriteString("- **System overview:** `specs/design/design.md`\n")
	}
	sb.WriteString("\n")

	// Org-published services (P3): the design marks this service consumable by
	// OTHER projects in the org, so its endpoint must be exposed cross-project.
	// This is per-task design data (not a static rule), so it's surfaced here in
	// the issue rather than only in the baked `asdlc` skill.
	if comp != nil && models.ResolveOrgPublished(*comp) {
		sb.WriteString("## Org-published service — endpoint visibility\n")
		sb.WriteString("This service is **published org-wide**: components in OTHER projects depend on it as an `org-service`. ")
		sb.WriteString("In your `workload.yaml`, the HTTP endpoint's `visibility` list MUST include **`namespace`** (in addition to `external`) — e.g. `visibility: [external, namespace]`. ")
		sb.WriteString("`namespace` visibility is what lets OpenChoreo resolve this endpoint for cross-project consumers; without it the dependent components in other projects cannot reach it.\n\n")
	}

	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("When you open the PR, include `Closes #%d` in its body so the platform links the PR back to this task. The full workflow, constraints, and deny-list are in the `asdlc` skill loaded in your Claude Code session.\n", task.IssueNumber))

	return sb.String()
}

// BuildOrgPublishIssueBody produces the markdown body for a cross-project
// publish task's GitHub issue (marketplace P3.5). Unlike BuildIssueBody this is
// NOT a build-from-design task: the provider component is already built and
// deployed, and the requested change is a single targeted edit — add `namespace`
// to its HTTP endpoint's `visibility` list in `workload.yaml` so OpenChoreo
// resolves it for cross-project consumers. The body is purpose-built (it does
// not depend on the design's `orgPublished` flag being set yet) and imperative,
// matching the tone of BuildIssueBody.
//
// The trailing `Closes #<this-issue>` reminder is restated for the
// human-on-GitHub audience and as a fail-safe if the `asdlc` skill isn't loaded.
// The issue number isn't known until GitHub mints it, so the body references
// "this issue" rather than a literal number (the agent reads the live number
// via `gh issue view`).
func BuildOrgPublishIssueBody(providerComponentName, appPath, requesterProject string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("> Access request from project `%s`: publish the **%s** service org-wide so it can be consumed across projects.\n\n",
		requesterProject, providerComponentName))

	sb.WriteString("## What to do\n")
	sb.WriteString("This is a **targeted change to an already-built component** — do NOT regenerate or re-implement anything.\n\n")
	sb.WriteString("1. Clone the repository.\n")
	if appPath != "" {
		sb.WriteString(fmt.Sprintf("2. Open `%s/workload.yaml`.\n", normalizeAppPath(appPath)))
	} else {
		sb.WriteString("2. Open the component's `workload.yaml`.\n")
	}
	sb.WriteString("3. Add `namespace` to the HTTP endpoint's `visibility` list — e.g. it should read `visibility: [external, namespace]`. ")
	sb.WriteString("`namespace` visibility is what lets OpenChoreo resolve this endpoint for cross-project consumers.\n")
	sb.WriteString("4. Do **NOT** change anything else — no code, no other config, no other endpoints.\n")
	sb.WriteString("5. Open a PR for this single-line change.\n\n")

	sb.WriteString("## Component Reference\n")
	sb.WriteString(fmt.Sprintf("- **Name:** %s\n", providerComponentName))
	if appPath != "" {
		sb.WriteString(fmt.Sprintf("- **App Path (within repo):** `%s`\n", normalizeAppPath(appPath)))
	}
	sb.WriteString("\n")

	sb.WriteString("---\n")
	sb.WriteString("When you open the PR, include `Closes #<this-issue>` in its body so the platform links the PR back to this task. The full workflow, constraints, and deny-list are in the `asdlc` skill loaded in your Claude Code session.\n")

	return sb.String()
}

// normalizeAppPath trims a single leading slash so the rendered Component
// Reference shows e.g. `hello-api` instead of `/hello-api`.
func normalizeAppPath(appPath string) string {
	return strings.TrimPrefix(appPath, "/")
}
