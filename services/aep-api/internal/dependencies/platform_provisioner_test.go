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

package dependencies

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
)

// ---- pure builders -----------------------------------------------------------

func TestBuildPlatformResource_ReferencesClusterResourceType(t *testing.T) {
	t.Parallel()

	r := BuildPlatformResource("shop", "maindb", "postgres-cnpg", map[string]any{"version": "16", "instances": 1})
	if r.Spec.Type.Kind != "ClusterResourceType" || r.Spec.Type.Name != "postgres-cnpg" {
		t.Fatalf("type ref: %+v", r.Spec.Type)
	}
	if r.Metadata.Name != "shop-maindb" {
		t.Fatalf("name: %s", r.Metadata.Name)
	}
	if r.Spec.Owner.ProjectName != "shop" {
		t.Fatalf("owner: %+v", r.Spec.Owner)
	}
	var got map[string]any
	_ = json.Unmarshal(r.Spec.Parameters, &got)
	if got["version"] != "16" {
		t.Fatalf("params: %s", r.Spec.Parameters)
	}
	// A number param must survive as a JSON number, NOT a quoted string — the
	// postgres-cnpg CRD declares `instances` integer, so a string-typed params
	// map (the pre-fix bug) would fail CRD validation. Unmarshalling into `any`
	// yields float64 for a JSON number and string for a quoted one.
	if n, ok := got["instances"].(float64); !ok || n != 1 {
		t.Fatalf("instances must round-trip as a JSON number, got %#v: %s", got["instances"], r.Spec.Parameters)
	}
}

func TestBuildPlatformResource_NoParamsOmitsParameters(t *testing.T) {
	t.Parallel()

	r := BuildPlatformResource("shop", "maindb", "postgres-cnpg", nil)
	if len(r.Spec.Parameters) != 0 {
		t.Fatalf("want no spec.parameters, got %s", r.Spec.Parameters)
	}
}

func TestBuildPlatformBinding_PinsRelease(t *testing.T) {
	t.Parallel()

	b, err := BuildPlatformBinding("shop", "maindb", "development", "shop-maindb-r1", map[string]any{"size": "small"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Spec.ResourceRelease != "shop-maindb-r1" || b.Spec.Environment != "development" {
		t.Fatalf("binding: %+v", b.Spec)
	}
	if b.Metadata.Name != "shop-maindb-development" {
		t.Fatalf("name: %s", b.Metadata.Name)
	}
	if b.Spec.Owner.ProjectName != "shop" || b.Spec.Owner.ResourceName != "shop-maindb" {
		t.Fatalf("owner: %+v", b.Spec.Owner)
	}
	var cfg map[string]string
	_ = json.Unmarshal(b.Spec.ResourceTypeEnvironmentConfigs, &cfg)
	if cfg["size"] != "small" {
		t.Fatalf("env configs: %s", b.Spec.ResourceTypeEnvironmentConfigs)
	}
}

// ---- OCNativeProvisioner ------------------------------------------------------

// newPlatformRC is the recording OC client for the platform provisioner: the
// applied Resource immediately reports a cut release (WaitForLatestRelease
// resolves on the first poll). EnsureResourceTypeFunc and GetBindingFunc are
// deliberately left nil — the moq mock PANICS on an unexpected call, which is
// the strongest proof that Provision NEVER authors the (discovered)
// ClusterResourceType and never waits on the binding's Ready condition.
func newPlatformRC(latest string) *ocmocks.ResourceClientMock {
	return &ocmocks.ResourceClientMock{
		ApplyResourceFunc: func(_ context.Context, _ string, r *openchoreo.Resource) (*openchoreo.Resource, error) {
			return r, nil
		},
		GetResourceFunc: func(_ context.Context, _, name string) (*openchoreo.Resource, error) {
			return &openchoreo.Resource{
				Metadata: openchoreo.OCObjectMeta{Name: name},
				Status:   &openchoreo.ResourceStatus{LatestRelease: &openchoreo.ResourceLatestRelease{Name: latest}},
			}, nil
		},
		EnsureBindingFunc: func(_ context.Context, _ string, b *openchoreo.ResourceReleaseBinding) (*openchoreo.ResourceReleaseBinding, error) {
			return b, nil
		},
		DeleteBindingFunc:  func(_ context.Context, _, _ string) error { return nil },
		DeleteResourceFunc: func(_ context.Context, _, _ string) error { return nil },
	}
}

func TestPlatformProvision_AuthorsResourceModelAsync(t *testing.T) {
	t.Parallel()

	rc := newPlatformRC("shop-maindb-r1")
	p := NewOCNativeProvisioner(rc)

	res, err := p.Provision(
		context.Background(),
		"default", "shop", "maindb", "postgres-cnpg",
		map[string]any{"version": "16"},
		[]string{"development", "production"},
	)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	// NEVER authors the type — it is discovered, not authored. (The nil
	// EnsureResourceTypeFunc would already have panicked on a call; the explicit
	// count keeps the invariant readable.)
	if calls := rc.EnsureResourceTypeCalls(); len(calls) != 0 {
		t.Fatalf("EnsureResourceType must NEVER be called, got %d calls", len(calls))
	}

	// Authored exactly one Resource against the discovered ClusterResourceType.
	applied := rc.ApplyResourceCalls()
	if len(applied) != 1 {
		t.Fatalf("want exactly 1 ApplyResource, got %d", len(applied))
	}
	if applied[0].R.Spec.Type.Kind != "ClusterResourceType" || applied[0].R.Spec.Type.Name != "postgres-cnpg" {
		t.Fatalf("type ref: %+v", applied[0].R.Spec.Type)
	}
	if applied[0].R.Metadata.Name != "shop-maindb" {
		t.Fatalf("resource name: %s", applied[0].R.Metadata.Name)
	}
	if len(rc.GetResourceCalls()) == 0 {
		t.Fatal("GetResource (poll for latestRelease) never called")
	}

	// One binding per env, pinned to the polled release. Provision returned
	// success WITHOUT any GetBinding readiness wait (nil GetBindingFunc).
	bindings := rc.EnsureBindingCalls()
	if len(bindings) != 2 {
		t.Fatalf("want 2 bindings, got %d", len(bindings))
	}
	for _, b := range bindings {
		if b.B.Spec.ResourceRelease != "shop-maindb-r1" {
			t.Errorf("binding not pinned: %q", b.B.Spec.ResourceRelease)
		}
	}
	if len(rc.GetBindingCalls()) != 0 {
		t.Fatal("Provision must not wait on binding readiness (GetBinding called)")
	}

	if res.ResourceName != "shop-maindb" {
		t.Errorf("result resource name: %s", res.ResourceName)
	}
	if res.BindingByEnv["development"] != "shop-maindb-development" || res.BindingByEnv["production"] != "shop-maindb-production" {
		t.Errorf("result bindings: %+v", res.BindingByEnv)
	}
}

// TestPlatformProvision_ReconcileWaitsForNewRelease proves the wait-for-CHANGE
// path: when ApplyResource reports a stale pre-reconcile release (an existing
// Resource being reconciled), Provision must NOT pin the binding to that stale
// release — it waits until the controller cuts a NEW one, then pins that.
func TestPlatformProvision_ReconcileWaitsForNewRelease(t *testing.T) {
	t.Parallel()

	var polls int32
	rc := &ocmocks.ResourceClientMock{
		// Reconcile: the applied Resource already carries the stale release.
		ApplyResourceFunc: func(_ context.Context, _ string, r *openchoreo.Resource) (*openchoreo.Resource, error) {
			r.Status = &openchoreo.ResourceStatus{LatestRelease: &openchoreo.ResourceLatestRelease{Name: "shop-maindb-r1"}}
			return r, nil
		},
		// First poll still reports the stale release; only later does the new one appear.
		GetResourceFunc: func(_ context.Context, _, name string) (*openchoreo.Resource, error) {
			rel := "shop-maindb-r1"
			if atomic.AddInt32(&polls, 1) >= 2 {
				rel = "shop-maindb-r2"
			}
			return &openchoreo.Resource{
				Metadata: openchoreo.OCObjectMeta{Name: name},
				Status:   &openchoreo.ResourceStatus{LatestRelease: &openchoreo.ResourceLatestRelease{Name: rel}},
			}, nil
		},
		EnsureBindingFunc: func(_ context.Context, _ string, b *openchoreo.ResourceReleaseBinding) (*openchoreo.ResourceReleaseBinding, error) {
			return b, nil
		},
	}
	p := NewOCNativeProvisioner(rc)
	p.pollInterval = time.Millisecond
	p.pollTimeout = time.Second

	res, err := p.Provision(context.Background(), "default", "shop", "maindb", "postgres-cnpg", nil, []string{"development"})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	for _, b := range rc.EnsureBindingCalls() {
		if b.B.Spec.ResourceRelease != "shop-maindb-r2" {
			t.Fatalf("binding must pin the NEW release, got %q", b.B.Spec.ResourceRelease)
		}
	}
	if res.BindingByEnv["development"] != "shop-maindb-development" {
		t.Errorf("result bindings: %+v", res.BindingByEnv)
	}
}

// A GetResource transport/5xx blip on a freshly created Resource (empty prior)
// is not a wait answer. Classifying every empty-prior wait error as permanent
// would stamp OC blips as Temporal NonRetryableApplicationError.
func TestPlatformProvision_GetResourceBlipIsNotPermanent(t *testing.T) {
	t.Parallel()

	rc := &ocmocks.ResourceClientMock{
		ApplyResourceFunc: func(_ context.Context, _ string, r *openchoreo.Resource) (*openchoreo.Resource, error) {
			return r, nil
		},
		GetResourceFunc: func(_ context.Context, _, _ string) (*openchoreo.Resource, error) {
			return nil, errors.New("connection reset")
		},
	}
	p := NewOCNativeProvisioner(rc)

	_, err := p.Provision(context.Background(), "default", "shop", "maindb", "postgres-cnpg", nil, []string{"development"})
	if err == nil {
		t.Fatal("want poll error when GetResource fails")
	}
	if errors.Is(err, ErrProvisionPermanent) {
		t.Fatalf("GetResource transport error must stay retryable, got %v", err)
	}
}

// ResourceTypeNotFound is terminal even when Apply returns an existing
// release: the type is gone, so waiting for a NEW release cannot succeed.
// Distinct from StaleReleaseWaitIsNotPermanent, which is ordinary deadline
// expiry on a still-valid type.
func TestPlatformProvision_ResourceTypeNotFoundWithPriorReleaseIsPermanent(t *testing.T) {
	t.Parallel()

	rc := &ocmocks.ResourceClientMock{
		ApplyResourceFunc: func(_ context.Context, _ string, r *openchoreo.Resource) (*openchoreo.Resource, error) {
			r.Status = &openchoreo.ResourceStatus{LatestRelease: &openchoreo.ResourceLatestRelease{Name: "shop-maindb-r1"}}
			return r, nil
		},
		GetResourceFunc: func(_ context.Context, _, name string) (*openchoreo.Resource, error) {
			return &openchoreo.Resource{
				Metadata: openchoreo.OCObjectMeta{Name: name},
				Status: &openchoreo.ResourceStatus{
					LatestRelease: &openchoreo.ResourceLatestRelease{Name: "shop-maindb-r1"},
					Conditions: []openchoreo.OCCondition{{
						Type:    "Ready",
						Status:  "False",
						Reason:  "ResourceTypeNotFound",
						Message: `ClusterResourceType "object-storage" not found`,
					}},
				},
			}, nil
		},
	}
	p := NewOCNativeProvisioner(rc)

	_, err := p.Provision(context.Background(), "default", "shop", "maindb", "object-storage", nil, []string{"development"})
	if !errors.Is(err, ErrProvisionPermanent) {
		t.Fatalf("want ErrProvisionPermanent on ResourceTypeNotFound even with a prior release, got %v", err)
	}
}

// ResourceTypeNotFound is a wait-boundary answer on a new Resource (empty
// prior): the controller will never cut a release.
func TestPlatformProvision_ResourceTypeNotFoundIsPermanent(t *testing.T) {
	t.Parallel()

	rc := &ocmocks.ResourceClientMock{
		ApplyResourceFunc: func(_ context.Context, _ string, r *openchoreo.Resource) (*openchoreo.Resource, error) {
			return r, nil
		},
		GetResourceFunc: func(_ context.Context, _, name string) (*openchoreo.Resource, error) {
			return &openchoreo.Resource{
				Metadata: openchoreo.OCObjectMeta{Name: name},
				Status: &openchoreo.ResourceStatus{
					Conditions: []openchoreo.OCCondition{{
						Type:    "Ready",
						Status:  "False",
						Reason:  "ResourceTypeNotFound",
						Message: `ClusterResourceType "object-storage" not found`,
					}},
				},
			}, nil
		},
	}
	p := NewOCNativeProvisioner(rc)

	_, err := p.Provision(context.Background(), "default", "shop", "maindb", "object-storage", nil, []string{"development"})
	if !errors.Is(err, ErrProvisionPermanent) {
		t.Fatalf("want ErrProvisionPermanent on ResourceTypeNotFound, got %v", err)
	}
}

func TestPlatformProvision_NeverCutReleaseIsPermanent(t *testing.T) {
	t.Parallel()

	rc := &ocmocks.ResourceClientMock{
		ApplyResourceFunc: func(_ context.Context, _ string, r *openchoreo.Resource) (*openchoreo.Resource, error) {
			return r, nil
		},
		GetResourceFunc: func(_ context.Context, _, name string) (*openchoreo.Resource, error) {
			return &openchoreo.Resource{Metadata: openchoreo.OCObjectMeta{Name: name}}, nil
		},
	}
	p := NewOCNativeProvisioner(rc)
	p.pollInterval = time.Millisecond
	p.pollTimeout = 20 * time.Millisecond

	_, err := p.Provision(context.Background(), "default", "shop", "maindb", "postgres-cnpg", nil, []string{"development"})
	if !errors.Is(err, ErrProvisionPermanent) {
		t.Fatalf("want ErrProvisionPermanent when latestRelease never appears, got %v", err)
	}
}

// A wait that expired while a stale release still sat on the Resource is not
// classified permanent: reconcile can be slow. Twin of NeverCutReleaseIsPermanent
// — Apply reports a prior release, Get keeps returning it.
func TestPlatformProvision_StaleReleaseWaitIsNotPermanent(t *testing.T) {
	t.Parallel()

	rc := &ocmocks.ResourceClientMock{
		ApplyResourceFunc: func(_ context.Context, _ string, r *openchoreo.Resource) (*openchoreo.Resource, error) {
			r.Status = &openchoreo.ResourceStatus{LatestRelease: &openchoreo.ResourceLatestRelease{Name: "shop-maindb-r1"}}
			return r, nil
		},
		GetResourceFunc: func(_ context.Context, _, name string) (*openchoreo.Resource, error) {
			return &openchoreo.Resource{
				Metadata: openchoreo.OCObjectMeta{Name: name},
				Status:   &openchoreo.ResourceStatus{LatestRelease: &openchoreo.ResourceLatestRelease{Name: "shop-maindb-r1"}},
			}, nil
		},
	}
	p := NewOCNativeProvisioner(rc)
	p.pollInterval = time.Millisecond
	p.pollTimeout = 20 * time.Millisecond

	_, err := p.Provision(context.Background(), "default", "shop", "maindb", "postgres-cnpg", nil, []string{"development"})
	if err == nil {
		t.Fatal("want timeout error when the stale release never changes")
	}
	if errors.Is(err, ErrProvisionPermanent) {
		t.Fatalf("a wait that expired on a stale release is not permanent (reconcile can be slow), got %v", err)
	}
}

func TestPlatformProvision_RequiresArgs(t *testing.T) {
	t.Parallel()

	p := NewOCNativeProvisioner(newPlatformRC("r1"))
	if _, err := p.Provision(context.Background(), "", "shop", "maindb", "postgres-cnpg", nil, []string{"development"}); err == nil {
		t.Fatal("expected error for empty orgHandle")
	}
	if _, err := p.Provision(context.Background(), "default", "shop", "maindb", "", nil, []string{"development"}); err == nil {
		t.Fatal("expected error for empty resourceType")
	}
}

// TestOCNativePlatformDeprovision asserts the teardown deletes each per-env
// binding (<project>-<dep>-<env>) and then the per-project Resource
// (<project>-<dep>) — the cleanup project deletion invokes to avoid orphans.
func TestOCNativePlatformDeprovision(t *testing.T) {
	t.Parallel()

	var deletedBindings, deletedResources []string
	rc := &ocmocks.ResourceClientMock{
		DeleteBindingFunc: func(_ context.Context, _, name string) error {
			deletedBindings = append(deletedBindings, name)
			return nil
		},
		DeleteResourceFunc: func(_ context.Context, _, name string) error {
			deletedResources = append(deletedResources, name)
			return nil
		},
	}
	p := NewOCNativeProvisioner(rc)
	if err := p.Deprovision(context.Background(), "default", "storefront-3", "orders-db", []string{"development", "production"}); err != nil {
		t.Fatalf("Deprovision: %v", err)
	}
	if got, want := strings.Join(deletedBindings, ","), "storefront-3-orders-db-development,storefront-3-orders-db-production"; got != want {
		t.Errorf("deleted bindings = %q, want %q", got, want)
	}
	if got, want := strings.Join(deletedResources, ","), "storefront-3-orders-db"; got != want {
		t.Errorf("deleted resource = %q, want %q", got, want)
	}
}

// TestOCNativePlatformDeprovision_CollectsErrors asserts errors are joined, not
// short-circuited: one failed env binding must not leave the Resource behind.
func TestOCNativePlatformDeprovision_CollectsErrors(t *testing.T) {
	t.Parallel()

	var deletedResources []string
	rc := &ocmocks.ResourceClientMock{
		DeleteBindingFunc: func(_ context.Context, _, name string) error {
			if strings.HasSuffix(name, "-development") {
				return errors.New("binding stuck")
			}
			return nil
		},
		DeleteResourceFunc: func(_ context.Context, _, name string) error {
			deletedResources = append(deletedResources, name)
			return nil
		},
	}
	p := NewOCNativeProvisioner(rc)
	err := p.Deprovision(context.Background(), "default", "shop", "maindb", []string{"development", "production"})
	if err == nil {
		t.Fatal("want the joined binding error surfaced")
	}
	if !strings.Contains(err.Error(), "development") {
		t.Errorf("error must name the failed env binding: %v", err)
	}
	if len(deletedResources) != 1 {
		t.Fatalf("Resource delete must still run after a failed binding delete, got %v", deletedResources)
	}
}
