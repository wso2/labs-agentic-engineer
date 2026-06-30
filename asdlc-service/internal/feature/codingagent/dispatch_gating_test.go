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

// Package codingagent — dispatch gating unit tests.
//
// These tests assert the post-A2c invariants:
//   - Same-project component dependencies still gate dispatch until the
//     sibling task reaches `deployed`.
//   - External connection dependencies still gate dispatch until the
//     config-collection task reaches `deployed`.
//   - Org-service (cross-project) dependencies do NOT gate dispatch;
//     block-at-proceed (A2b) already guarantees a consumer cannot be
//     dispatched while an org-service dep is unresolved.
package codingagent

import (
	"testing"

	"github.com/wso2/asdlc/asdlc-service/models"
)

// ---- helpers ----------------------------------------------------------------

func makeTask(name string, deps, connDeps, orgServiceDeps []string) *models.ComponentTask {
	return &models.ComponentTask{
		ComponentName:        name,
		DependsOnComponents:  models.StringSlice(deps),
		DependsOnConnections: models.StringSlice(connDeps),
		DependsOnOrgServices: models.StringSlice(orgServiceDeps),
	}
}

// ---- depsAllDeployed unit tests ---------------------------------------------

// TestDepsAllDeployed_NoDeps: a task with no deps is always dispatchable.
func TestDepsAllDeployed_NoDeps(t *testing.T) {
	task := makeTask("frontend", nil, nil, nil)
	if !depsAllDeployed(task, nil, nil, nil) {
		t.Fatal("want true for a task with no deps, got false")
	}
}

// TestDepsAllDeployed_ComponentDepMet: a task whose same-project component dep
// is `deployed` should be dispatchable.
func TestDepsAllDeployed_ComponentDepMet(t *testing.T) {
	task := makeTask("frontend", []string{"backend-api"}, nil, nil)
	statusByComp := map[string]string{"backend-api": string(models.TaskStatusDeployed)}
	if !depsAllDeployed(task, statusByComp, nil, nil) {
		t.Fatal("want true when component dep is deployed, got false")
	}
}

// TestDepsAllDeployed_ComponentDepNotMet: a task whose same-project component
// dep is NOT yet `deployed` must be held (depsAllDeployed returns false).
// This is the core same-project gating contract that A2c must NOT break.
func TestDepsAllDeployed_ComponentDepNotMet(t *testing.T) {
	task := makeTask("frontend", []string{"backend-api"}, nil, nil)

	// dep is pending — not deployed.
	statusByComp := map[string]string{"backend-api": string(models.TaskStatusPending)}
	if depsAllDeployed(task, statusByComp, nil, nil) {
		t.Fatal("want false when component dep is pending (not yet deployed), got true")
	}
}

// TestDepsAllDeployed_ComponentDepUnknown: an unknown component name fails
// closed (false), protecting against misconfiguration.
func TestDepsAllDeployed_ComponentDepUnknown(t *testing.T) {
	task := makeTask("frontend", []string{"backend-api"}, nil, nil)
	// statusByComp is empty — dep is unknown.
	if depsAllDeployed(task, map[string]string{}, nil, nil) {
		t.Fatal("want false for unknown component dep (fail-closed), got true")
	}
}

// TestDepsAllDeployed_ConnectionDepMet: a task whose connection dep's config-
// collection task is `deployed` should be dispatchable.
func TestDepsAllDeployed_ConnectionDepMet(t *testing.T) {
	task := makeTask("worker", nil, []string{"stripe"}, nil)
	statusByConn := map[string]string{"stripe": string(models.TaskStatusDeployed)}
	if !depsAllDeployed(task, nil, statusByConn, nil) {
		t.Fatal("want true when connection dep is deployed, got false")
	}
}

// TestDepsAllDeployed_ConnectionDepNotMet: a task whose connection dep is not
// yet provisioned must be held.
func TestDepsAllDeployed_ConnectionDepNotMet(t *testing.T) {
	task := makeTask("worker", nil, []string{"stripe"}, nil)
	statusByConn := map[string]string{"stripe": string(models.TaskStatusPending)}
	if depsAllDeployed(task, nil, statusByConn, nil) {
		t.Fatal("want false when connection dep is not deployed, got true")
	}
}

// TestDepsAllDeployed_OrgServiceDepDoesNotGate is the key A2c regression test:
// a consumer task that lists a DependsOnOrgServices entry must be treated as
// immediately dispatchable — the org-service dep no longer gates dispatch.
// Under block-at-proceed (A2b) the consumer cannot have reached this point
// without its org-service deps already being `resolved`.
func TestDepsAllDeployed_OrgServiceDepDoesNotGate(t *testing.T) {
	// Consumer has an org-service dep (cross-project), no same-project deps.
	task := makeTask("consumer", nil, nil, []string{"payment-service"})
	// Both maps are empty — there is no "payment-service" entry anywhere.
	// Post-A2c: this must return true (org-service deps are not checked here).
	if !depsAllDeployed(task, map[string]string{}, map[string]string{}, nil) {
		t.Fatal("A2c regression: org-service dep must NOT gate dispatch (block-at-proceed guarantees pre-resolution); want true, got false")
	}
}

// TestDepsAllDeployed_OrgServiceAndComponentMixed: a consumer with BOTH a
// same-project component dep and an org-service dep. Only the component dep
// gates; the org-service dep is ignored.
func TestDepsAllDeployed_OrgServiceAndComponentMixed(t *testing.T) {
	task := makeTask("consumer", []string{"local-api"}, nil, []string{"payment-service"})
	statusByComp := map[string]string{"local-api": string(models.TaskStatusDeployed)}

	// org-service dep ("payment-service") is unknown — must still pass because
	// the component dep is deployed and org-service no longer gates.
	if !depsAllDeployed(task, statusByComp, nil, nil) {
		t.Fatal("want true when same-project dep deployed and org-service dep present but ignored; got false")
	}
}

// TestDepsAllDeployed_OrgServiceAndComponentMixed_ComponentNotDeployed: if the
// same-project component dep is NOT deployed, the task is held regardless of
// the org-service dep state.
func TestDepsAllDeployed_OrgServiceAndComponentMixed_ComponentNotDeployed(t *testing.T) {
	task := makeTask("consumer", []string{"local-api"}, nil, []string{"payment-service"})
	// local-api is in_progress, not deployed.
	statusByComp := map[string]string{"local-api": string(models.TaskStatusInProgress)}

	if depsAllDeployed(task, statusByComp, nil, nil) {
		t.Fatal("want false when same-project component dep is not deployed; got true")
	}
}

// TestDepsAllDeployed_AllDepsDeployed: a task with all dep types (component +
// connection + org-service) is dispatchable when component and connection deps
// are deployed (regardless of org-service state).
func TestDepsAllDeployed_AllDepsDeployed(t *testing.T) {
	task := makeTask("consumer",
		[]string{"backend"},
		[]string{"postgres"},
		[]string{"payment-service"},
	)
	statusByComp := map[string]string{"backend": string(models.TaskStatusDeployed)}
	statusByConn := map[string]string{"postgres": string(models.TaskStatusDeployed)}

	if !depsAllDeployed(task, statusByComp, statusByConn, nil) {
		t.Fatal("want true when component and connection deps are deployed; got false")
	}
}

// TestDepsAllDeployed_GatesOnResource: a component task with a platform-resource
// dep is held until the resource-provisioning task reaches `deployed`.
func TestDepsAllDeployed_GatesOnResource(t *testing.T) {
	task := &models.ComponentTask{DependsOnResources: models.StringSlice{"maindb"}}
	byComp := map[string]string{}
	byConn := map[string]string{}
	notReady := map[string]string{"maindb": string(models.TaskStatusBuilding)}
	if depsAllDeployed(task, byComp, byConn, notReady) {
		t.Fatal("should gate while provisioning")
	}
	ready := map[string]string{"maindb": string(models.TaskStatusDeployed)}
	if !depsAllDeployed(task, byComp, byConn, ready) {
		t.Fatal("should dispatch when deployed")
	}
}
