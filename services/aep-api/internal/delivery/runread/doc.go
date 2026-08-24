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

// Package runread is the milestone run's READ surface: a version's runs and
// their cycles, two SSE streams over those cycles' agent logs — one per RUN and
// one per VERSION — and the cancel write behind them.
//
// Two streams because there are two DIFFERENT questions: a run is one execution,
// and a version is several of them (a dev run delivers it, a task run repairs a
// defect in it, a validation run re-judges it). They share their frame walk and
// differ in when they end.
//
// It is a read model, so it owns no state and decides nothing. Everything it
// serves is either a row the supervisor and the event plane already wrote, or a
// pod log — and cancel is a single call straight through to the supervisor.
//
// Two data planes, priced separately (design §10): run rows and cycle records
// are webhook-fed DB reads that cost nothing to poll, while the GitHub-backed
// issue list lives on the tasks read and is polled only while a run is live.
// Nothing here touches GitHub.
//
// Why a slice of its own rather than a corner of `run`: `run` is the Temporal
// supervisor, and the read surface must not drag a workflow engine into an HTTP
// GET. It names its collaborators as ports and imports only the delivery root —
// `RunStatus`, `MilestoneRunWorkflowID` and the run/cycle entities live there
// precisely so this package can use them without importing a sibling slice.
package runread
