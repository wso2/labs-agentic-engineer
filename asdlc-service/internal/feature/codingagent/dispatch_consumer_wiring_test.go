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

// Package codingagent — consumer wiring renderer unit tests (A6).
//
// These tests assert that resolveConsumerDependenciesYAML renders a
// `dependencies.resources[]` block for platform-resource deps (DependsOnResources)
// with the <DEP>_<OUTPUT> env-var naming convention. No store, no OC client,
// no DB — pure YAML rendering driven by a fake binding reader.
package codingagent

import (
	"context"
	"strings"
	"testing"

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// fakePlatformBindingReader returns a hard-coded binding for a known name and
// nil for everything else. The binding name format is <project>-<depName>-<env>.
func fakePlatformBindingReader(bindings map[string]*openchoreo.ResourceReleaseBinding) ConnectionBindingReader {
	return func(_ context.Context, _, name string) (*openchoreo.ResourceReleaseBinding, error) {
		b, ok := bindings[name]
		if !ok {
			return nil, nil
		}
		return b, nil
	}
}

// bindingWithOutputs builds a ready ResourceReleaseBinding with the given output names.
func bindingWithOutputs(names ...string) *openchoreo.ResourceReleaseBinding {
	outputs := make([]openchoreo.ResolvedOutput, 0, len(names))
	for _, n := range names {
		outputs = append(outputs, openchoreo.ResolvedOutput{Name: n})
	}
	return &openchoreo.ResourceReleaseBinding{
		Status: &openchoreo.ResourceReleaseBindingStatus{
			Outputs: outputs,
		},
	}
}

// TestResolveConsumerDependenciesYAML_PlatformResourceDep asserts that for a
// task with DependsOnResources: ["db"], the rendered YAML includes a
// `dependencies.resources[]` entry with ref: <project>-db and env bindings
// host→DB_HOST, password→DB_PASSWORD (<DEP>_<OUTPUT> uppercased convention).
func TestResolveConsumerDependenciesYAML_PlatformResourceDep(t *testing.T) {
	const (
		orgID     = "acme"
		projectID = "todo"
		depName   = "db"
	)
	// Platform resource binding name: <project>-<depName>-development
	bindingName := projectID + "-" + depName + "-development"

	reader := fakePlatformBindingReader(map[string]*openchoreo.ResourceReleaseBinding{
		bindingName: bindingWithOutputs("host", "password"),
	})

	svc := &dispatchService{
		connBindingReader: reader,
		// orgServiceResolver is nil: skips design-component resolution entirely.
	}

	task := &models.ComponentTask{
		OrgID:              orgID,
		ProjectID:          projectID,
		ComponentName:      "todo-app",
		DependsOnResources: models.StringSlice{depName},
	}

	got, err := svc.resolveConsumerDependenciesYAML(context.Background(), task)
	if err != nil {
		t.Fatalf("resolveConsumerDependenciesYAML error: %v", err)
	}
	if got == "" {
		t.Fatal("expected non-empty YAML, got empty string")
	}

	// Expect the ref entry for <project>-<depName>.
	wantRef := "ref: " + projectID + "-" + depName
	if !strings.Contains(got, wantRef) {
		t.Errorf("YAML missing %q:\n%s", wantRef, got)
	}

	// Expect <DEP>_<OUTPUT> env var naming: host→DB_HOST, password→DB_PASSWORD.
	type envCheck struct {
		output string
		envVar string
	}
	for _, c := range []envCheck{
		{"host", "DB_HOST"},
		{"password", "DB_PASSWORD"},
	} {
		// The YAML key is the output name, the value is the env var.
		wantBinding := c.output + ": " + c.envVar
		if !strings.Contains(got, wantBinding) {
			t.Errorf("YAML missing env binding %q (want output %q → env %q):\n%s",
				wantBinding, c.output, c.envVar, got)
		}
	}
}

// TestResolveConsumerDependenciesYAML_PlatformResourceDep_SkipsUnprovisioned
// asserts that a platform-resource dep with no ready binding is silently skipped
// (returns "" when no other deps are present).
func TestResolveConsumerDependenciesYAML_PlatformResourceDep_SkipsUnprovisioned(t *testing.T) {
	// Reader returns nil for every name — dep not provisioned yet.
	reader := fakePlatformBindingReader(nil)

	svc := &dispatchService{connBindingReader: reader}

	task := &models.ComponentTask{
		OrgID:              "acme",
		ProjectID:          "todo",
		ComponentName:      "todo-app",
		DependsOnResources: models.StringSlice{"db"},
	}

	got, err := svc.resolveConsumerDependenciesYAML(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string when binding unprovisioned, got:\n%s", got)
	}
}

// TestResolveConsumerDependenciesYAML_PlatformResourceDep_MultipleOutputs
// asserts that all outputs are rendered with the correct <DEP>_<OUTPUT> prefix.
func TestResolveConsumerDependenciesYAML_PlatformResourceDep_MultipleOutputs(t *testing.T) {
	const (
		projectID = "myproj"
		depName   = "maindb"
	)
	bindingName := projectID + "-" + depName + "-development"
	reader := fakePlatformBindingReader(map[string]*openchoreo.ResourceReleaseBinding{
		bindingName: bindingWithOutputs("host", "port", "user", "password"),
	})

	svc := &dispatchService{connBindingReader: reader}

	task := &models.ComponentTask{
		OrgID:              "org1",
		ProjectID:          projectID,
		ComponentName:      "worker",
		DependsOnResources: models.StringSlice{depName},
	}

	got, err := svc.resolveConsumerDependenciesYAML(context.Background(), task)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	for _, c := range []struct{ out, env string }{
		{"host", "MAINDB_HOST"},
		{"port", "MAINDB_PORT"},
		{"user", "MAINDB_USER"},
		{"password", "MAINDB_PASSWORD"},
	} {
		want := c.out + ": " + c.env
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
