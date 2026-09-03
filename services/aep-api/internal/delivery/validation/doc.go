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

// Package validation owns the VALIDATION phase's platform side: it mints the
// validation task (armed, kind `validation`) that the milestone run's validation cycle is
// dispatched at.
//
// ONE issue per VERSION, filed into that version's milestone by the create
// itself. The milestone is the version pin — there is no version label, and no
// follow-up patch, so the issue is never versionless. Per version rather than per
// project because the issue body embeds the validation criteria as they stood at
// mint time: reusing the previous version's issue would hand this version's agent
// the previous version's oracle.
//
// The run supervisor mints it at DEPLOYED-GREEN, not at plan time: an issue
// that nothing can work until every component is deployed would otherwise sit
// in the run's working set and hold every cycle boundary open. It is prose
// carrying one label, and it deliberately does NOT carry the `aep` working-set
// label for the same reason — the cycle is dispatched at it by number.
//
// This feature does the ISSUE side only. Deployed endpoint URLs are never written
// into this (public) issue: the runner PREFLIGHTS them from the secure
// validation-context endpoint before its agent starts.
//
// TEST CREDENTIALS do not come from this domain at all. The build provisions the
// project's test users (the identity domain, from `specs/design/security.json`) and
// publishes each account's login as a comment on the ROLES GATE ticket, which is
// where the agent reads it (ADR-0022). That leaves exactly one published copy of
// each password, so no two places can disagree about it.
//
// The callback identifies its caller by the run CYCLE the platform dispatched
// (context.go's CycleLocator) — the id the pod carries as AEP_TASK_ID and the
// subject its bearer is bound to. Resolving it anywhere else is not a detail: it
// was resolved against the executions table, which the milestone supervisor does
// not write, so every validation runner was told its own dispatch did not exist
// and the endpoint resolution below never ran.
//
// Feature-edge allowlist: {gitrepo}. Design + criteria reads are consumer ports
// (ports.go) satisfied by adapters at the composition root, so this package
// imports neither the artifacts nor the files feature.
package validation
