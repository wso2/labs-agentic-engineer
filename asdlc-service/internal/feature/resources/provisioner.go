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

package resources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
)

// ResourceProvisioner is the narrow swap-point of P5: it authors the OpenChoreo
// Resource model for a platform-resource dependency and returns immediately —
// provisioning a real backing instance (a database, a broker) is asynchronous,
// so Provision MUST NOT block on readiness. It authors the Resource + per-env
// binding and returns; the resource watcher (a later task) observes the
// binding's native Ready condition out-of-band.
//
// This is the ONLY abstraction that varies by environment. The OC-native
// implementation below authors directly against openchoreo-api in both local
// and cloud. PlatformAPIProvisioner (reserved, see below) is the future cloud
// swap-in. Everything else in P5 — discovery, the task graph, the watcher, the
// drawer, consumption — is provisioner-agnostic.
type ResourceProvisioner interface {
	// Provision authors (idempotently) the OC Resource model for a platform-
	// resource dep and returns immediately — it does NOT wait for readiness
	// (a real provisioner is async; the resource watcher observes the binding's
	// native Ready condition). resourceType is a DISCOVERED ClusterResourceType
	// name. params are user-supplied provisioning parameters (spec.parameters).
	Provision(ctx context.Context, orgHandle, projectName, depName, resourceType string,
		params map[string]string, envs []string) (*ProvisionResult, error)

	// Deprovision tears down what Provision authored: the per-env bindings (their
	// retainPolicy cascades the rendered backing instance — for postgres-cnpg the
	// CNPG Cluster + its generated Secret/PVC), then the Resource. Best-effort and
	// 404-tolerant — used by project deletion, where the Resource/binding would
	// otherwise orphan (the OC Project delete does NOT cascade them; they carry a
	// logical spec.owner.projectName, not a k8s ownerReference).
	Deprovision(ctx context.Context, orgHandle, projectName, depName string, envs []string) error
}

// ProvisionResult reports what was authored: the per-project Resource name and
// the per-env binding names. It carries NO outputs — outputs (incl. secret
// references) are resolved later from the binding's status by the readiness
// watcher, never returned synchronously from the request path.
type ProvisionResult struct {
	ResourceName string
	BindingByEnv map[string]string
}

// PlatformAPIProvisioner is the RESERVED cloud implementation (wso2cloud #638).
// When that provisioning API ships, this impl calls it to mint the backing
// instance, then materializes the returned outputs into the SAME OC binding so
// consumption stays identical. NOT IMPLEMENTED in P5 — the OCNativeProvisioner
// is the only registered impl; this comment marks the seam.

// OCNativeProvisioner authors the OC Resource model directly against
// openchoreo-api (identical local + cloud). It references a DISCOVERED
// ClusterResourceType — it NEVER authors the type (the cluster PE installs it
// via the deployments/ scripts) and so makes no EnsureResourceType call.
//
// Flow (async): ApplyResource → poll status.latestRelease (cutting a release is
// fast) → author the per-env binding pinned to that release → return. It does
// NOT wait for the binding's Ready condition: standing up a real DB takes
// minutes; the resource watcher observes Ready out-of-band.
type OCNativeProvisioner struct {
	rc openchoreo.ResourceClient

	// pollInterval / pollTimeout bound ONLY the wait for the controller to cut
	// the ResourceRelease (Resources have no AutoDeploy — the BFF pins it).
	// This is NOT a readiness wait — it is the fast "did a release name appear"
	// poll, just long enough to pin the binding.
	pollInterval time.Duration
	pollTimeout  time.Duration
}

func NewOCNativeProvisioner(rc openchoreo.ResourceClient) *OCNativeProvisioner {
	return &OCNativeProvisioner{
		rc:           rc,
		pollInterval: 2 * time.Second,
		pollTimeout:  60 * time.Second,
	}
}

// Provision authors the Resource + per-env bindings and returns immediately. It
// references the discovered ClusterResourceType `resourceType` (no
// EnsureResourceType), writes user params to spec.parameters, polls only for
// status.latestRelease to pin, and returns success even when the binding is not
// yet Ready (async — the watcher observes readiness later).
func (p *OCNativeProvisioner) Provision(
	ctx context.Context,
	orgHandle, projectName, depName, resourceType string,
	params map[string]string,
	envs []string,
) (*ProvisionResult, error) {
	if orgHandle == "" || projectName == "" || depName == "" {
		return nil, fmt.Errorf("resources: orgHandle, projectName and depName required")
	}
	if resourceType == "" {
		return nil, fmt.Errorf("resources: resourceType (ClusterResourceType name) required")
	}

	// 1. Resource (one per project; references the discovered ClusterResourceType).
	// NO EnsureResourceType — app-factory never authors the type.
	res := BuildPlatformResource(projectName, depName, resourceType, params)
	if _, err := p.rc.ApplyResource(ctx, orgHandle, res); err != nil {
		return nil, fmt.Errorf("resources: apply resource: %w", err)
	}

	// 2. Wait for status.latestRelease (no AutoDeploy → BFF pins it). This is a
	// fast poll for the release NAME, not a readiness wait.
	latest, err := p.waitForLatestRelease(ctx, orgHandle, res.Metadata.Name)
	if err != nil {
		return nil, err
	}

	// 3. Per env: author the binding pinned to latestRelease. Return without
	// waiting on the binding Ready condition — a real DB takes minutes.
	result := &ProvisionResult{ResourceName: res.Metadata.Name, BindingByEnv: map[string]string{}}
	for _, env := range envs {
		binding, berr := BuildPlatformBinding(projectName, depName, env, latest, params)
		if berr != nil {
			return nil, berr
		}
		if _, err := p.rc.EnsureBinding(ctx, orgHandle, binding); err != nil {
			return nil, fmt.Errorf("resources: ensure binding for env %q: %w", env, err)
		}
		result.BindingByEnv[env] = binding.Metadata.Name
	}
	return result, nil
}

// waitForLatestRelease polls the Resource for status.latestRelease — the release
// name the per-env bindings pin to. Reused from the connections provisioner: it
// bounds ONLY the (fast) release-cut, never readiness of the backing instance.
func (p *OCNativeProvisioner) waitForLatestRelease(ctx context.Context, namespace, resourceName string) (string, error) {
	deadline := time.Now().Add(p.pollTimeout)
	for {
		got, err := p.rc.GetResource(ctx, namespace, resourceName)
		if err != nil {
			return "", fmt.Errorf("resources: poll resource %q: %w", resourceName, err)
		}
		if got.Status != nil && got.Status.LatestRelease != nil && got.Status.LatestRelease.Name != "" {
			return got.Status.LatestRelease.Name, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("resources: resource %q produced no ResourceRelease within %s", resourceName, p.pollTimeout)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(p.pollInterval):
		}
	}
}

// Deprovision deletes the per-env bindings (their retainPolicy cascades the
// rendered CNPG Cluster + generated Secret/PVC), then the Resource. Best-effort:
// DeleteBinding/DeleteResource are 404-tolerant, and errors are collected rather
// than short-circuited so one failed env doesn't leave the Resource behind.
func (p *OCNativeProvisioner) Deprovision(ctx context.Context, orgHandle, projectName, depName string, envs []string) error {
	resourceName := platformResourceName(projectName, depName)
	var errs []error
	for _, env := range envs {
		if err := p.rc.DeleteBinding(ctx, orgHandle, resourceName+"-"+env); err != nil {
			errs = append(errs, fmt.Errorf("delete binding (%s/%s): %w", depName, env, err))
		}
	}
	if err := p.rc.DeleteResource(ctx, orgHandle, resourceName); err != nil {
		errs = append(errs, fmt.Errorf("delete resource (%s): %w", depName, err))
	}
	return errors.Join(errs...)
}

// ---- pure builders (unit-tested) -------------------------------------------

// platformResourceName is the per-project Resource name (metadata.name is
// namespace-unique; owner.projectName does NOT scope it).
func platformResourceName(project, depName string) string { return project + "-" + depName }

// BuildPlatformResource references a DISCOVERED ClusterResourceType (Type.Kind =
// "ClusterResourceType") — unlike the connections provisioner's namespaced
// per-connection ResourceType, app-factory never authors this type. User
// provisioning params go to spec.parameters as JSON.
func BuildPlatformResource(project, depName, rtName string, params map[string]string) *openchoreo.Resource {
	res := &openchoreo.Resource{
		Metadata: openchoreo.OCObjectMeta{Name: platformResourceName(project, depName)},
		Spec: openchoreo.ResourceSpec{
			Owner: openchoreo.ResourceOwner{ProjectName: project},
			Type:  openchoreo.ResourceTypeRef{Kind: "ClusterResourceType", Name: rtName},
		},
	}
	if len(params) > 0 {
		if raw, err := json.Marshal(params); err == nil {
			res.Spec.Parameters = raw
		}
	}
	return res
}

// BuildPlatformBinding authors a per-env binding pinned to latestRelease. Per-env
// params go to spec.resourceTypeEnvironmentConfigs as JSON.
func BuildPlatformBinding(project, depName, env, latestRelease string, params map[string]string) (*openchoreo.ResourceReleaseBinding, error) {
	resourceName := platformResourceName(project, depName)
	b := &openchoreo.ResourceReleaseBinding{
		Metadata: openchoreo.OCObjectMeta{Name: resourceName + "-" + env},
		Spec: openchoreo.ResourceReleaseBindingSpec{
			Owner:           openchoreo.ResourceReleaseBindingOwner{ProjectName: project, ResourceName: resourceName},
			Environment:     env,
			ResourceRelease: latestRelease,
		},
	}
	if len(params) > 0 {
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("resources: marshal env configs: %w", err)
		}
		b.Spec.ResourceTypeEnvironmentConfigs = json.RawMessage(raw)
	}
	return b, nil
}

// OCNativeProvisioner is the only registered ResourceProvisioner impl in P5.
var _ ResourceProvisioner = (*OCNativeProvisioner)(nil)
