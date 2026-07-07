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

// Command demo drives DevelopmentFlowWorkflows end-to-end against the local dev
// server, with an in-process worker wired to fakes (design: api, web→api).
//
//	go run ./cmd/demo            # auto: a human-gated and an autonomous cycle
//	go run ./cmd/demo -manual    # you approve the gates (terminal Enter OR Web UI)
//
// In -manual mode the program never sends the approval signals; it polls the
// cycle's phase, so it advances whether you press Enter here or click
// "Send a Signal" in the Temporal Web UI. Build/deploy events (not human gates)
// are still driven automatically.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"go.temporal.io/sdk/client"
	sdkworker "go.temporal.io/sdk/worker"

	"github.com/wso2/labs-agentic-engineer/packages/contracts/orchestration"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/activities"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/activities/fake"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/types"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/workflows"
)

const org, project = "acme", "web"

func main() {
	manual := flag.Bool("manual", false, "approve gates yourself (terminal Enter or Web UI) instead of auto")
	flag.Parse()

	c, err := client.Dial(client.Options{HostPort: "localhost:7233", Namespace: "default"})
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	acts := activities.New(
		&fake.Design{Default: []activities.ComponentSpec{
			{Name: "api"},
			{Name: "web", DependsOn: []string{"api"}},
		}},
		&fake.Checker{Result: types.GateChecksResult{Passed: true}},
		fake.NewDispatcher(),
		fake.NewMerger(),
	)
	w := sdkworker.New(c, orchestration.TaskQueue, sdkworker.Options{})
	w.RegisterWorkflow(workflows.DevelopmentFlowWorkflow)
	w.RegisterWorkflow(workflows.TaskLifecycleWorkflow)
	w.RegisterActivity(acts)
	if err := w.Start(); err != nil {
		log.Fatal(err)
	}
	defer w.Stop()

	ctx := context.Background()
	if *manual {
		runManualCycle(ctx, c, "manual-1")
	} else {
		fmt.Println("════════ CYCLE 1: human gates (auto-approved) ════════")
		runCycle(ctx, c, "demo-human", allHuman(), false)
		fmt.Println("\n════════ CYCLE 2: autonomous (all gates auto) ════════")
		runCycle(ctx, c, "demo-auto", allAuto(), true)
	}
	fmt.Println("\nTemporal Web UI → http://localhost:8233 (namespace 'default')")
}

func allHuman() types.GatePolicy {
	return types.GatePolicy{Requirements: orchestration.GateHuman, Design: orchestration.GateHuman, CodeReview: orchestration.GateHuman}
}
func allAuto() types.GatePolicy {
	return types.GatePolicy{Requirements: orchestration.GateAuto, Design: orchestration.GateAuto, CodeReview: orchestration.GateAuto}
}

// runManualCycle starts a human-gated cycle and waits for YOU to approve each
// gate — by pressing Enter here, or via the Web UI "Send a Signal" action.
func runManualCycle(ctx context.Context, c client.Client, cycle string) {
	wfID := orchestration.DevFlowWorkflowID(org, project, cycle)
	if _, err := c.ExecuteWorkflow(ctx,
		client.StartWorkflowOptions{ID: wfID, TaskQueue: orchestration.TaskQueue},
		workflows.DevelopmentFlowWorkflow,
		types.DevelopmentFlowInput{
			Org: org, Project: project, CycleID: cycle,
			Source: orchestration.SourceRequirement, StartPhase: orchestration.PhaseRequirements, GatePolicy: allHuman(),
		}); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Cycle started: %s\n", wfID)
	fmt.Println("Approve each gate by pressing [Enter] here, OR in the Web UI")
	fmt.Println("(http://localhost:8233 → open the workflow → 'Send a Signal').")

	lines := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()

	waitForGate(ctx, c, wfID, lines, orchestration.PhaseRequirements, orchestration.SignalApproveRequirements)
	waitForGate(ctx, c, wfID, lines, orchestration.PhaseDesign, orchestration.SignalApproveDesign)

	// Implement phase: tasks are system-driven (PR/build/deploy), not human gates.
	fmt.Println("\n▶ design approved → IMPLEMENT: driving task events automatically…")
	driveTask(ctx, c, "api")
	driveTask(ctx, c, "web")

	waitForGate(ctx, c, wfID, lines, orchestration.PhaseMerge, orchestration.SignalMarkComplete)

	var final types.CycleStateView
	if err := c.GetWorkflow(ctx, wfID, "").Get(ctx, &final); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\n✅ %s → phase=%s tasks=%s\n", cycle, final.Phase, fmtTasks(final.Tasks))
}

// waitForGate first waits for the cycle to REACH `expected` (the prior approval
// takes a moment to advance the phase), then blocks until the cycle LEAVES it —
// either because you pressed Enter (we send `approve` for you) or because you
// sent it from the Web UI (we detect the phase moved on). Polls once a second.
func waitForGate(ctx context.Context, c client.Client, wfID string, lines <-chan string, expected orchestration.Phase, approve string) {
	for phaseOf(ctx, c, wfID) != expected {
		time.Sleep(300 * time.Millisecond)
	}
	fmt.Printf("\n⏸  phase=%s — press [Enter] to send %s (or send it from the Web UI)\n", expected, approve)
	for phaseOf(ctx, c, wfID) == expected {
		select {
		case <-lines:
			if err := c.SignalWorkflow(ctx, wfID, "", approve, nil); err != nil {
				log.Fatal(err)
			}
			fmt.Printf("   → sent %s (from terminal)\n", approve)
			return
		case <-time.After(time.Second):
		}
	}
	fmt.Printf("   → advanced (signal sent from the Web UI)\n")
}

func phaseOf(ctx context.Context, c client.Client, wfID string) orchestration.Phase {
	val, err := c.QueryWorkflow(ctx, wfID, "", orchestration.QueryGetCycleState)
	if err != nil {
		log.Fatal(err)
	}
	var st types.CycleStateView
	if err := val.Get(&st); err != nil {
		log.Fatal(err)
	}
	return st.Phase
}

// ---- auto-mode helpers (default run) ----

func runCycle(ctx context.Context, c client.Client, cycle string, policy types.GatePolicy, autonomous bool) {
	wfID := orchestration.DevFlowWorkflowID(org, project, cycle)
	step("START %s", wfID)
	if _, err := c.ExecuteWorkflow(ctx,
		client.StartWorkflowOptions{ID: wfID, TaskQueue: orchestration.TaskQueue},
		workflows.DevelopmentFlowWorkflow,
		types.DevelopmentFlowInput{
			Org: org, Project: project, CycleID: cycle,
			Source: orchestration.SourceRequirement, StartPhase: orchestration.PhaseRequirements, GatePolicy: policy,
		}); err != nil {
		log.Fatal(err)
	}
	if autonomous {
		step("(no approvals — auto gates advance via passing checks)")
		time.Sleep(800 * time.Millisecond)
	} else {
		signal(ctx, c, wfID, orchestration.SignalApproveRequirements, "approve requirements")
		signal(ctx, c, wfID, orchestration.SignalApproveDesign, "approve design")
	}
	show(ctx, c, wfID)
	driveTask(ctx, c, "api")
	show(ctx, c, wfID)
	driveTask(ctx, c, "web")
	show(ctx, c, wfID)
	signal(ctx, c, wfID, orchestration.SignalMarkComplete, "mark complete")
	var final types.CycleStateView
	if err := c.GetWorkflow(ctx, wfID, "").Get(ctx, &final); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("✅ %s → phase=%s tasks=%s\n", cycle, final.Phase, fmtTasks(final.Tasks))
}

func step(f string, a ...any) { fmt.Printf("▶ "+f+"\n", a...) }

func signal(ctx context.Context, c client.Client, wfID, sig, desc string) {
	step("signal %s — %s", sig, desc)
	if err := c.SignalWorkflow(ctx, wfID, "", sig, nil); err != nil {
		log.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
}

// driveTask sends the signals taking a task child to deployed: PR → build → deploy.
func driveTask(ctx context.Context, c client.Client, task string) {
	id := orchestration.TaskWorkflowID(org, project, task)
	step("drive task %q: PR → build → deploy → deployed", task)
	for _, sig := range []string{
		orchestration.SignalPRReady, orchestration.SignalPRMerged,
		orchestration.SignalBuildStarted, orchestration.SignalBuildSucceeded,
		orchestration.SignalDeployStarted, orchestration.SignalDeploySucceeded,
	} {
		time.Sleep(300 * time.Millisecond)
		if err := c.SignalWorkflow(ctx, id, "", sig, nil); err != nil {
			log.Fatalf("signal %s -> %s: %v", sig, id, err)
		}
	}
	time.Sleep(500 * time.Millisecond)
}

func show(ctx context.Context, c client.Client, wfID string) {
	var st types.CycleStateView
	val, err := c.QueryWorkflow(ctx, wfID, "", orchestration.QueryGetCycleState)
	if err != nil {
		log.Fatal(err)
	}
	if err := val.Get(&st); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("    └─ phase=%s tasks=%s\n", st.Phase, fmtTasks(st.Tasks))
}

func fmtTasks(ts []types.TaskStateView) string {
	if len(ts) == 0 {
		return "[]"
	}
	out := ""
	for _, t := range ts {
		out += fmt.Sprintf("%s:%s ", t.TaskID, t.Status)
	}
	return "[" + out + "]"
}
