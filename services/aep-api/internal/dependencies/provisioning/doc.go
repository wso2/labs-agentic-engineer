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

// Package provisioning is the coordinating feature for dependency provisioning
// on our GitHub-native funnel (dependency-management §3.6). It rebuilds the
// value/param-collection surface upstream expressed as component_tasks + a
// projector, re-based onto `provision` GitHub issues + a provision Execution
// kind + the funnel's dependency-kind-aware gate.
//
// It owns:
//
//   - EnsureProvisionIssues — mint one `provision` gate issue per distinct
//     external / platform-resource dependency in the approved design (deduped
//     per project). The consumer coding task holds until each derives deployed.
//   - ValueService — external value collection: split values by the registered
//     schema, write secrets to SM-API + author the OC external Resource model,
//     then Finish the provision Execution + close the issue
//     (external readyWhen:${true} → completion is synchronous).
//   - ResourceService — platform-resource provisioning: author the OC Resource +
//     binding (async) and admit a running provision Execution; the readiness
//     watcher observes the binding's native Ready condition out-of-band.
//   - ResourceWatcher — the resource-readiness watcher (cloned from the exec
//     watcher): on the binding going Ready it Finishes the provision Execution
//     (→ deployed) and closes the issue with the binding reference. A 30-minute
//     stale bound fails a stuck run.
//   - StatusService — the masked status read (outputs by name only — secrets
//     live only in SM-API / the OC-rendered Secret, never in a response).
//
// The no-secrets invariant holds throughout: secret VALUES go only to SM-API /
// OpenBao; issue bodies, comments, and API responses carry only names / paths /
// references. This package coordinates provisioners (dependencies/resources) +
// GitHub issues (gitrepo); every other collaborator (executions store, design
// read, repo lookup) is a narrow local port wired in the composition root.
package provisioning
