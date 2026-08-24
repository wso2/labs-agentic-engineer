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

package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/wso2/aep/aectl/internal/addons"
	"github.com/wso2/aep/aectl/internal/ui"
)

// fakeApplier is a manifestApplier that records every ApplyYAML call and
// answers Exists from a pre-populated set.
type fakeApplier struct {
	applied  []string        // YAML strings passed to ApplyYAML, in order
	existing map[string]bool // "Kind/Name" → whether Exists returns true
}

func (f *fakeApplier) ApplyYAML(_ context.Context, _, _, manifest string) error {
	f.applied = append(f.applied, manifest)
	return nil
}

func (f *fakeApplier) Exists(_ context.Context, _, kind, _, name string) (bool, error) {
	return f.existing[kind+"/"+name], nil
}

// selectByID returns a multiSelect func that selects the addon with the given
// ID. It fails the test immediately if no such addon exists in addons.Available.
func selectByID(t *testing.T, id string) func(string, []ui.SelectItem) ([]bool, bool) {
	t.Helper()
	idx := -1
	for i, a := range addons.Available {
		if a.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		t.Fatalf("addon %q not found in addons.Available", id)
	}
	return func(_ string, items []ui.SelectItem) ([]bool, bool) {
		sel := make([]bool, len(items))
		sel[idx] = true
		return sel, true
	}
}

// addonByID returns the Addon with the given ID from addons.Available.
// It fails the test immediately if the addon is absent.
func addonByID(t *testing.T, id string) addons.Addon {
	t.Helper()
	for _, a := range addons.Available {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("addon %q not found in addons.Available", id)
	return addons.Addon{}
}

// existingForAddon pre-populates the fakeApplier with all VerifyResources of a.
func existingForAddon(a addons.Addon) map[string]bool {
	m := make(map[string]bool, len(a.VerifyResources))
	for _, v := range a.VerifyResources {
		m[v.Kind+"/"+v.Name] = true
	}
	return m
}

// TestRunAddonInstall_DeclinedConfirmation verifies that when the user
// declines the operator-install prompt, installAddons returns nil and never
// calls newApplier or applies any manifest.
func TestRunAddonInstall_DeclinedConfirmation(t *testing.T) {
	newApplierCalled := false
	deps := addonDeps{
		multiSelect: selectByID(t, "thunder-app"),
		confirm:     func(string) bool { return false },
		installOperator: func(context.Context, string, addons.OperatorSpec) error {
			t.Error("installOperator must not be called when confirmation is declined")
			return nil
		},
		newApplier: func(string) (manifestApplier, error) {
			newApplierCalled = true
			return nil, errors.New("should not be called")
		},
	}

	if err := runAddonInstall(context.Background(), "", deps); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if newApplierCalled {
		t.Error("newApplier must not be called when operator confirmation is declined")
	}
}

// TestRunAddonInstall_OperatorFailureSkipsAddon verifies that when an operator
// install fails, its dependent addon is skipped and the function returns nil
// (other addons that succeed would still be applied; here only one is selected).
func TestRunAddonInstall_OperatorFailureSkipsAddon(t *testing.T) {
	first := addonByID(t, "thunder-app")

	fa := &fakeApplier{existing: existingForAddon(first)}
	installCalled := false
	newApplierCalls := 0
	deps := addonDeps{
		multiSelect: selectByID(t, "thunder-app"),
		confirm:     func(string) bool { return true },
		installOperator: func(_ context.Context, _ string, op addons.OperatorSpec) error {
			installCalled = true
			return errors.New("simulated operator install failure")
		},
		newApplier: func(string) (manifestApplier, error) {
			newApplierCalls++
			return fa, nil
		},
	}

	if err := runAddonInstall(context.Background(), "", deps); err != nil {
		t.Fatalf("expected nil (failed operator is not a fatal error), got %v", err)
	}
	if !installCalled {
		t.Error("installOperator must be called")
	}
	if newApplierCalls != 0 {
		t.Errorf("newApplier called %d time(s), want 0 — applier must not be built when all operators fail", newApplierCalls)
	}
	if len(fa.applied) != 0 {
		t.Errorf("expected no manifests applied after operator failure, got %d", len(fa.applied))
	}
}

// TestRunAddonInstall_SuccessAppliesManifests verifies that when the operator
// installs successfully, all manifests for the selected addon are applied.
func TestRunAddonInstall_SuccessAppliesManifests(t *testing.T) {
	first := addonByID(t, "thunder-app")

	fa := &fakeApplier{existing: existingForAddon(first)}
	installCalled := false
	deps := addonDeps{
		multiSelect: selectByID(t, "thunder-app"),
		confirm:     func(string) bool { return true },
		installOperator: func(context.Context, string, addons.OperatorSpec) error {
			installCalled = true
			return nil
		},
		newApplier: func(string) (manifestApplier, error) {
			return fa, nil
		},
	}

	if err := runAddonInstall(context.Background(), "", deps); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !installCalled {
		t.Error("installOperator must be called on the success path")
	}
	if got, want := len(fa.applied), len(first.Manifests); got != want {
		t.Errorf("applied %d manifests, want %d", got, want)
	}
}

// TestRunAddonInstall_OperatorVersion verifies how the resolved platform version
// flows into each operator's Helm --version:
//   - an addon whose OperatorSpec omits Version inherits a non-empty platformVersion;
//   - an addon with an explicit Version keeps it (platformVersion is ignored);
//   - an empty platformVersion leaves an unset Version empty (no --version pin).
func TestRunAddonInstall_OperatorVersion(t *testing.T) {
	tests := []struct {
		name            string
		addonID         string
		platformVersion string
		wantVersion     string
	}{
		{
			name:            "empty operator version inherits platform version",
			addonID:         "thunder-app", // OperatorSpec.Version == ""
			platformVersion: "0.6.0-rc.17",
			wantVersion:     "0.6.0-rc.17",
		},
		{
			name:            "explicit operator version is preserved",
			addonID:         "postgres-cnpg", // OperatorSpec.Version == "0.29.0"
			platformVersion: "0.6.0-rc.17",
			wantVersion:     "0.29.0",
		},
		{
			name:            "empty platform version leaves unset version empty",
			addonID:         "thunder-app",
			platformVersion: "",
			wantVersion:     "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := addonByID(t, tc.addonID)
			fa := &fakeApplier{existing: existingForAddon(a)}
			var got addons.OperatorSpec
			deps := addonDeps{
				multiSelect: selectByID(t, tc.addonID),
				confirm:     func(string) bool { return true },
				installOperator: func(_ context.Context, _ string, op addons.OperatorSpec) error {
					got = op
					return nil
				},
				newApplier: func(string) (manifestApplier, error) { return fa, nil },
			}

			if err := runAddonInstall(context.Background(), tc.platformVersion, deps); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Version != tc.wantVersion {
				t.Errorf("operator Version = %q, want %q", got.Version, tc.wantVersion)
			}
		})
	}
}
