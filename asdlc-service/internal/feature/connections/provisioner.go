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

package connections

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// SecretWriter is the slice of the SM-API writer the provisioner needs — the
// (a)-synthesis seam. Satisfied by orgcreds.SMAPIWriter.
type SecretWriter interface {
	Enabled() bool
	// WriteConnectionSecret uploads the secret fields for a (project, entity) and
	// returns the Vault KV path the ExternalSecret reads (the secretStorePath).
	WriteConnectionSecret(ctx context.Context, ocOrgID, projectName, entityName string, data map[string]string) (vaultKey, secretRefName string, err error)
}

// EnvConnectionValues are the user-supplied values for one environment.
type EnvConnectionValues struct {
	Plain  map[string]string // non-secret key → value
	Secret map[string]string // secret key → value
}

// Provisioner authors the OpenChoreo Resource model for a registered connection
// in a project (the (a)-synthesis flow, plan §6): SM-API value write →
// ResourceType (get-or-create) → Resource (controller cuts a ResourceRelease) →
// per-env ResourceReleaseBinding pinned to status.latestRelease.
type Provisioner struct {
	registry *Registry
	rc       openchoreo.ResourceClient
	sm       SecretWriter

	// pollInterval / pollTimeout bound the wait for the controller to cut the
	// ResourceRelease (Resources have no AutoDeploy — we poll status.latestRelease).
	pollInterval time.Duration
	pollTimeout  time.Duration
}

func NewProvisioner(registry *Registry, rc openchoreo.ResourceClient, sm SecretWriter) *Provisioner {
	return &Provisioner{
		registry:     registry,
		rc:           rc,
		sm:           sm,
		pollInterval: 2 * time.Second,
		pollTimeout:  60 * time.Second,
	}
}

// ProvisionResult reports what was authored.
type ProvisionResult struct {
	ResourceName  string            // the per-project Resource name
	LatestRelease string            // the pinned ResourceRelease
	BindingByEnv  map[string]string // env → binding name
}

// ProvisionConnection authors (idempotently) the connection's Resource model for
// a project across the given environments. `orgHandle` is the OC namespace the
// CRs live in; `ocOrgID` is the SM-API org id. Per env it writes the secret
// values (when any) to SM-API and pins the per-env binding with the resulting
// secretStorePath + plain values.
func (p *Provisioner) ProvisionConnection(
	ctx context.Context,
	orgHandle, ocOrgID, projectName string,
	conn *models.Connection,
	byEnv map[string]EnvConnectionValues,
) (*ProvisionResult, error) {
	if conn == nil {
		return nil, fmt.Errorf("connections: nil connection")
	}
	if orgHandle == "" || projectName == "" {
		return nil, fmt.Errorf("connections: orgHandle and projectName required")
	}

	// 1. ResourceType (get-or-create; immutable once created).
	rt, err := openchoreo.BuildExternalConnectionResourceType(conn.ResourceTypeName, toConnConfigKeys(conn.ConfigSchema))
	if err != nil {
		return nil, fmt.Errorf("connections: build resourcetype: %w", err)
	}
	if _, err := p.rc.EnsureResourceType(ctx, orgHandle, rt); err != nil {
		return nil, fmt.Errorf("connections: ensure resourcetype %q: %w", conn.ResourceTypeName, err)
	}

	// 2. Resource (one per project; controller cuts the ResourceRelease).
	res := buildConnectionResource(projectName, conn)
	if _, err := p.rc.ApplyResource(ctx, orgHandle, res); err != nil {
		return nil, fmt.Errorf("connections: apply resource: %w", err)
	}

	// 3. Wait for status.latestRelease (no AutoDeploy → BFF pins it).
	latest, err := p.waitForLatestRelease(ctx, orgHandle, res.Metadata.Name)
	if err != nil {
		return nil, err
	}

	// 4. Per env: SM-API secret write → per-env binding pinned to latestRelease.
	result := &ProvisionResult{ResourceName: res.Metadata.Name, LatestRelease: latest, BindingByEnv: map[string]string{}}
	for env, vals := range byEnv {
		var secretStorePath string
		if len(vals.Secret) > 0 {
			if !p.sm.Enabled() {
				return nil, fmt.Errorf("connections: SM-API not configured but connection %q has secret values", conn.Name)
			}
			entity := connectionSecretEntity(conn.Name, env)
			path, _, werr := p.sm.WriteConnectionSecret(ctx, ocOrgID, projectName, entity, vals.Secret)
			if werr != nil {
				return nil, fmt.Errorf("connections: write secret for env %q: %w", env, werr)
			}
			secretStorePath = path
		}
		binding, berr := buildConnectionBinding(projectName, conn.Name, env, latest, secretStorePath, vals.Plain)
		if berr != nil {
			return nil, berr
		}
		if _, err := p.rc.EnsureBinding(ctx, orgHandle, binding); err != nil {
			return nil, fmt.Errorf("connections: ensure binding for env %q: %w", env, err)
		}
		result.BindingByEnv[env] = binding.Metadata.Name
	}
	return result, nil
}

// Deprovision is the 2-step delete: per-env bindings first (their retainPolicy
// cascades the DP ConfigMap/ExternalSecret), then the Resource (its finalizer
// blocks until bindings are gone). The ResourceType is long-lived — never deleted.
func (p *Provisioner) Deprovision(ctx context.Context, orgHandle, projectName, connName string, envs []string) error {
	resourceName := connectionResourceName(projectName, connName)
	for _, env := range envs {
		if err := p.rc.DeleteBinding(ctx, orgHandle, resourceName+"-"+env); err != nil {
			return fmt.Errorf("connections: delete binding (%s/%s): %w", connName, env, err)
		}
	}
	if err := p.rc.DeleteResource(ctx, orgHandle, resourceName); err != nil {
		return fmt.Errorf("connections: delete resource (%s): %w", connName, err)
	}
	return nil
}

func (p *Provisioner) waitForLatestRelease(ctx context.Context, namespace, resourceName string) (string, error) {
	deadline := time.Now().Add(p.pollTimeout)
	for {
		got, err := p.rc.GetResource(ctx, namespace, resourceName)
		if err != nil {
			return "", fmt.Errorf("connections: poll resource %q: %w", resourceName, err)
		}
		if got.Status != nil && got.Status.LatestRelease != nil && got.Status.LatestRelease.Name != "" {
			return got.Status.LatestRelease.Name, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("connections: resource %q produced no ResourceRelease within %s", resourceName, p.pollTimeout)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(p.pollInterval):
		}
	}
}

// ---- pure builders (unit-tested) -------------------------------------------

// connectionResourceName is the per-project Resource name (metadata.name is
// namespace-unique; owner.projectName does NOT scope it).
func connectionResourceName(project, conn string) string { return project + "-" + conn }

// connectionSecretEntity is the SM-API EntityName for a connection's per-env
// secret bundle — one SM-API secret per (project, connection, env).
func connectionSecretEntity(conn, env string) string { return "conn-" + conn + "-" + env }

func buildConnectionResource(projectName string, conn *models.Connection) *openchoreo.Resource {
	return &openchoreo.Resource{
		Metadata: openchoreo.OCObjectMeta{Name: connectionResourceName(projectName, conn.Name)},
		Spec: openchoreo.ResourceSpec{
			Owner: openchoreo.ResourceOwner{ProjectName: projectName},
			Type:  openchoreo.ResourceTypeRef{Kind: "ResourceType", Name: conn.ResourceTypeName},
		},
	}
}

func buildConnectionBinding(projectName, connName, env, latestRelease, secretStorePath string, plain map[string]string) (*openchoreo.ResourceReleaseBinding, error) {
	cfg := map[string]string{}
	for k, v := range plain {
		cfg[k] = v
	}
	if secretStorePath != "" {
		cfg[openchoreo.SecretStorePathField] = secretStorePath
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("connections: marshal env configs: %w", err)
	}
	resourceName := connectionResourceName(projectName, connName)
	return &openchoreo.ResourceReleaseBinding{
		Metadata: openchoreo.OCObjectMeta{Name: resourceName + "-" + env},
		Spec: openchoreo.ResourceReleaseBindingSpec{
			Owner:                          openchoreo.ResourceReleaseBindingOwner{ProjectName: projectName, ResourceName: resourceName},
			Environment:                    env,
			ResourceRelease:                latestRelease,
			ResourceTypeEnvironmentConfigs: json.RawMessage(raw),
		},
	}, nil
}

func toConnConfigKeys(in []models.ConfigKey) []openchoreo.ConnectionConfigKey {
	out := make([]openchoreo.ConnectionConfigKey, 0, len(in))
	for _, k := range in {
		out = append(out, openchoreo.ConnectionConfigKey{Key: k.Key, Secret: k.Secret})
	}
	return out
}
