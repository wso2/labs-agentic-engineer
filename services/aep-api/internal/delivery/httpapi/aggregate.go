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

package httpapi

import (
	"github.com/wso2/aep/aep-api/internal/delivery/build"
	"github.com/wso2/aep/aep-api/internal/delivery/execution"
	"github.com/wso2/aep/aep-api/internal/delivery/runread"
	"github.com/wso2/aep/aep-api/internal/delivery/task"
)

// Deps carries every service the delivery slice handlers call. It lives in the
// aggregator (not the delivery root) because delivery's services live in
// sub-packages the root may not import — the httpapi aggregator is the one place
// allowed to name them.
type Deps struct {
	BuildSvc     *build.Service
	PreflightSvc *build.PreflightService
	// BuildActivity records the spec_published feed line on build start
	// (issue #239); nil disables recording.
	BuildActivity build.SpecPublishedRecorder
	TaskReads     *task.Reads
	TaskCommands  *task.Commands
	TaskStream    *execution.TaskStreamService

	// The milestone run read surface: a version's runs + cycles, the per-run
	// progress stream, and cancel.
	RunReads       *runread.Reads
	RunProgress    *runread.ProgressService
	RunCommands    *runread.Commands
	RunCycleBuilds *runread.CycleBuilds

	// PublisherProvisioner is nil in tests that do not care. Wired on
	// Handler via WithPublisherProvisioner so StartProjectBuild cannot see it.
	PublisherProvisioner build.PublisherProvisioner
}

// Every slice names its type Handler, so embedding them directly would be
// "Handler redeclared". Local aliases give distinct field names (§6).
type (
	buildHandler     = build.Handler
	taskHandler      = task.Handler
	executionHandler = execution.Handler
	runreadHandler   = runread.Handler
)

// Handlers is the delivery domain's slice handlers, embedded so Go promotes each
// operation exactly once into the edge's composite. It declares nothing.
type Handlers struct {
	*buildHandler
	*taskHandler
	*executionHandler
	*runreadHandler
}

// New assembles the domain: pure wiring, constructor injection only. The
// handlers are fail-LOUD on a wired-but-nil service and 503 on an entirely
// unwired one (each slice's nil guard), matching the pre-migration edge.
func New(d Deps) (*Handlers, error) {
	bh := build.NewHandler(d.BuildSvc, d.PreflightSvc, d.BuildActivity)
	if d.PublisherProvisioner != nil {
		bh = bh.WithPublisherProvisioner(d.PublisherProvisioner)
	}
	return &Handlers{
		buildHandler:     bh,
		taskHandler:      task.NewHandler(d.TaskReads, d.TaskCommands),
		executionHandler: execution.NewHandler(d.TaskStream),
		runreadHandler:   runread.NewHandler(d.RunReads, d.RunProgress, d.RunCommands, d.RunCycleBuilds),
	}, nil
}
