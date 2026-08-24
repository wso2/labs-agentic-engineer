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

// Package delivery is the Delivery Pipeline domain (§3.3): it implements a
// versioned Spec end-to-end — plan Tasks, route Executions through the ONE
// funnel, dispatch coding agents, build/deploy components, and run validation.
//
// Its internal shape is the shared-kernel-ROOT + feature-sub-package layout of
// §10.3.1: anything referenced across feature boundaries is a TYPE or PORT that
// lives HERE in the root, and each feature (validation, and — as the fold
// proceeds — execution, taskflow, buildpipe, codingdispatch, devflowwork) is a
// sub-package importing only the root. That layout is what preserves the §1
// task ⊥ execution boundary as an internal one (taskflow and execution are peer
// sub-packages sharing only the root Dispatcher port + taskmeta + executions
// rows) while making every former feature→feature edge a legal slice→root type
// reference.
//
// The root is intentionally near-empty today: `validation` (S2S validation
// context/credentials, no cross-edges) is the first feature to land and needs
// no shared kernel. The kernel fills in as execution/devflow arrive.
//
// # The issue-write surface
//
// One piece of the kernel is BEHAVIOUR rather than a type: IssueWriter
// (issue_writer.go), through which every issue the domain mints, closes,
// reopens, comments on or labels is written. It is here for the same reason the
// label vocabulary is — the event plane, the plan turn, the build click and
// validation all file issues, and no two of them may import each other — and
// keeping it here is what makes a change to the label vocabulary or the dedupe
// contract one edit instead of one per sub-package. Detection stays with the
// detector; only the write moved.
package delivery
