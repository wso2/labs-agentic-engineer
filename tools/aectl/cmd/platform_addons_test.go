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
	onApply  func(manifest string)
}

func (f *fakeApplier) ApplyYAML(_ context.Context, _, _, manifest string) error {
	f.applied = append(f.applied, manifest)
	if f.onApply != nil {
		f.onApply(manifest)
	}
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
	deps := addonDeps{
		multiSelect: selectByID(t, "thunder-app"),
		confirm:     func(string) bool { return true },
		installOperator: func(_ context.Context, _ string, op addons.OperatorSpec) error {
			installCalled = true
			return errors.New("simulated operator install failure")
		},
		newApplier: func(string) (manifestApplier, error) {
			return fa, nil
		},
	}

	if err := runAddonInstall(context.Background(), "", deps); err != nil {
		t.Fatalf("expected nil (failed operator is not a fatal error), got %v", err)
	}
	if !installCalled {
		t.Error("installOperator must be called")
	}
	// When operator fails, only pre-manifests are applied (zero for thunder-app
	// since credentials are now provisioned by the platform chart, not PreManifests).
	if got, want := len(fa.applied), len(first.Operator.PreManifests); got != want {
		t.Errorf("applied %d manifest(s), want %d (pre-manifests only)", got, want)
	}
}

// TestRunAddonInstall_SuccessAppliesManifests verifies that when the operator
// installs successfully, all manifests for the selected addon are applied and
// that every PreManifests apply event occurs before installOperator is called.
func TestRunAddonInstall_SuccessAppliesManifests(t *testing.T) {
	first := addonByID(t, "thunder-app")

	var events []string
	fa := &fakeApplier{
		existing: existingForAddon(first),
		onApply:  func(_ string) { events = append(events, "apply") },
	}
	installCalled := false
	deps := addonDeps{
		multiSelect: selectByID(t, "thunder-app"),
		confirm:     func(string) bool { return true },
		installOperator: func(context.Context, string, addons.OperatorSpec) error {
			installCalled = true
			events = append(events, "install")
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
	wantApplied := len(first.Operator.PreManifests) + len(first.Manifests)
	if got := len(fa.applied); got != wantApplied {
		t.Errorf("applied %d manifests, want %d (pre-manifests + addon manifests)", got, wantApplied)
	}

	// Assert ordering: every PreManifests apply event precedes the install event.
	installIdx := -1
	for i, e := range events {
		if e == "install" {
			installIdx = i
			break
		}
	}
	if installIdx == -1 {
		t.Fatal("install event not found in event log")
	}
	preApplyCount := 0
	for i := 0; i < installIdx; i++ {
		if events[i] == "apply" {
			preApplyCount++
		}
	}
	if preApplyCount != len(first.Operator.PreManifests) {
		t.Errorf("pre-manifest applies before install = %d, want %d", preApplyCount, len(first.Operator.PreManifests))
	}
}

// TestRunAddonInstall_OperatorWaitsForSecrets verifies that when an operator's
// OperatorSpec.WaitForSecrets is non-empty, deps.waitForSecrets is called before
// installOperator and a successful sync allows the operator to install.
func TestRunAddonInstall_OperatorWaitsForSecrets(t *testing.T) {
	first := addonByID(t, "thunder-app")
	fa := &fakeApplier{existing: existingForAddon(first)}

	var events []string
	deps := addonDeps{
		multiSelect: selectByID(t, "thunder-app"),
		confirm:     func(string) bool { return true },
		waitForSecrets: func(_ context.Context, namespace string, names []string) error {
			events = append(events, "wait")
			return nil
		},
		installOperator: func(context.Context, string, addons.OperatorSpec) error {
			events = append(events, "install")
			return nil
		},
		newApplier: func(string) (manifestApplier, error) { return fa, nil },
	}

	if err := runAddonInstall(context.Background(), "", deps); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	waitIdx, installIdx := -1, -1
	for i, e := range events {
		if e == "wait" && waitIdx == -1 {
			waitIdx = i
		}
		if e == "install" && installIdx == -1 {
			installIdx = i
		}
	}
	if waitIdx == -1 {
		t.Error("waitForSecrets must be called when WaitForSecrets is non-empty")
	}
	if installIdx == -1 {
		t.Error("installOperator must be called on the success path")
	}
	if waitIdx != -1 && installIdx != -1 && waitIdx > installIdx {
		t.Error("waitForSecrets must be called before installOperator")
	}
}

// TestRunAddonInstall_SecretSyncTimeout verifies that when waitForSecrets
// returns an error (e.g. ESO sync timed out), installOperator is not called
// and the failure is recorded in operatorFailed so the addon is skipped.
func TestRunAddonInstall_SecretSyncTimeout(t *testing.T) {
	first := addonByID(t, "thunder-app")
	fa := &fakeApplier{existing: existingForAddon(first)}
	installCalled := false

	deps := addonDeps{
		multiSelect: selectByID(t, "thunder-app"),
		confirm:     func(string) bool { return true },
		waitForSecrets: func(_ context.Context, _ string, _ []string) error {
			return errors.New("timed out waiting for thunder-app-operator-credentials")
		},
		installOperator: func(context.Context, string, addons.OperatorSpec) error {
			installCalled = true
			return nil
		},
		newApplier: func(string) (manifestApplier, error) { return fa, nil },
	}

	// A secret sync failure is non-fatal to the overall install — operatorFailed
	// records it and the addon is skipped, but the function returns nil.
	if err := runAddonInstall(context.Background(), "", deps); err != nil {
		t.Fatalf("expected nil (sync failure is non-fatal), got %v", err)
	}
	if installCalled {
		t.Error("installOperator must not be called when secret sync fails")
	}
	// No addon manifests should have been applied either (operator failed → addon skipped).
	if len(fa.applied) != 0 {
		t.Errorf("applied %d manifests, want 0 (addon skipped after sync failure)", len(fa.applied))
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
