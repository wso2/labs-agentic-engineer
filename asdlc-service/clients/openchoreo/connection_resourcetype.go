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

package openchoreo

import (
	"encoding/json"
	"fmt"
)

// ConnectionConfigKey is one env-var key in a connection's schema. Mirrors the
// agents/BFF ConfigKey without importing models (keeps the OC client leaf-level).
type ConnectionConfigKey struct {
	Key    string
	Secret bool
}

// Per-env value field names the BFF supplies on the binding's
// resourceTypeEnvironmentConfigs (and the ResourceType's environmentConfigs
// schema). Plain keys are supplied verbatim by their own key name; all secret
// values live in a single SM-API secret addressed by SecretStorePathField, read
// per-property by the ExternalSecret.
const (
	// SecretStorePathField is the environmentConfig holding the SM-API/OpenBao
	// KV path where this connection's secret values live for the environment.
	SecretStorePathField = "secretStorePath"
	// connConfigMapID / connSecretID are the ResourceType manifest ids.
	connConfigMapID = "config"
	connSecretID    = "secret"
)

// BuildExternalConnectionResourceType turns a connection's config key schema
// into a per-connection ResourceType (plan §6, the "external SaaS connection
// pattern" — no upstream sample exists, so this is modeled on the shipped
// `postgres` ResourceType + the `${dataplane.secretStore}` ExternalSecret form).
//
// It renders, per consuming environment:
//   - a ConfigMap holding the plain (non-secret) values, fed from
//     environmentConfigs (one ConfigMap key per plain config key; always emitted
//     so the resources list is non-empty — OC requires MinItems=1);
//   - an ESO ExternalSecret (only when ≥1 secret key) that pulls the secret
//     values from the data plane's store (`secretStoreRef: ${dataplane.secretStore}`)
//     at the per-env SM-API path (environmentConfigs.secretStorePath), one
//     `data[]` entry per secret key (property == the key name);
//   - explicit `readyWhen` (a ConfigMap/ExternalSecret-only Resource is not Ready
//     by default — without it the consumer gates forever);
//   - `outputs[]` mapping each key to a consumer-bindable value: plain →
//     configMapKeyRef, secret → secretKeyRef. The consumer binds these via
//     Workload.spec.dependencies.resources[].envBindings.
//
// `name` is the connection name (e.g. "salesforce"); the caller sets the
// namespace + a suffix (immutability) via EnsureResourceType. ResourceTypes are
// effectively immutable — a changed key schema must use a new name.
func BuildExternalConnectionResourceType(name string, keys []ConnectionConfigKey) (*ResourceType, error) {
	if name == "" {
		return nil, fmt.Errorf("connection resourcetype: empty name")
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("connection resourcetype %q: at least one config key required", name)
	}

	var plain, secret []ConnectionConfigKey
	for _, k := range keys {
		if k.Key == "" {
			return nil, fmt.Errorf("connection resourcetype %q: empty config key", name)
		}
		if k.Secret {
			secret = append(secret, k)
		} else {
			plain = append(plain, k)
		}
	}

	// environmentConfigs schema: each plain key (string) + the secret store path
	// (string) when there are secrets. These are the per-env values the BFF
	// supplies on the binding's resourceTypeEnvironmentConfigs.
	props := map[string]any{}
	for _, k := range plain {
		props[k.Key] = map[string]any{"type": "string"}
	}
	if len(secret) > 0 {
		props[SecretStorePathField] = map[string]any{"type": "string"}
	}
	envConfigSchema := &SchemaSection{OpenAPIV3Schema: map[string]any{
		"type":       "object",
		"properties": props,
	}}

	// ── ConfigMap (always) — non-secret values. ─────────────────────────────
	cmData := map[string]any{}
	for _, k := range plain {
		cmData[k.Key] = "${environmentConfigs." + k.Key + "}"
	}
	if len(cmData) == 0 {
		// Keep the ConfigMap non-empty when the connection is all-secret.
		cmData["_ready"] = "true"
	}
	cmTemplate, err := toRawTemplate(map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      "${metadata.name}-" + connConfigMapID,
			"namespace": "${metadata.namespace}",
			"labels":    "${metadata.labels}",
		},
		"data": cmData,
	})
	if err != nil {
		return nil, err
	}
	resources := []ResourceTypeManifest{{
		ID:        connConfigMapID,
		ReadyWhen: "${true}", // an applied ConfigMap is ready immediately
		Template:  cmTemplate,
	}}

	outputs := make([]ResourceTypeOutput, 0, len(keys))
	for _, k := range plain {
		outputs = append(outputs, ResourceTypeOutput{
			Name:            k.Key,
			ConfigMapKeyRef: &OCKeyRef{Name: "${metadata.name}-" + connConfigMapID, Key: k.Key},
		})
	}

	// ── ExternalSecret (only when there are secrets). ───────────────────────
	if len(secret) > 0 {
		esData := make([]map[string]any, 0, len(secret))
		for _, k := range secret {
			esData = append(esData, map[string]any{
				"secretKey": k.Key,
				"remoteRef": map[string]any{
					"key":      "${environmentConfigs." + SecretStorePathField + "}",
					"property": k.Key,
				},
			})
		}
		esTemplate, terr := toRawTemplate(map[string]any{
			"apiVersion": "external-secrets.io/v1",
			"kind":       "ExternalSecret",
			"metadata": map[string]any{
				"name":      "${metadata.name}-" + connSecretID,
				"namespace": "${metadata.namespace}",
				"labels":    "${metadata.labels}",
			},
			"spec": map[string]any{
				"refreshInterval": "1m",
				"secretStoreRef": map[string]any{
					"name": "${dataplane.secretStore}",
					"kind": "ClusterSecretStore",
				},
				"target": map[string]any{
					"name":           "${metadata.name}-" + connSecretID,
					"creationPolicy": "Owner",
				},
				"data": esData,
			},
		})
		if terr != nil {
			return nil, terr
		}
		resources = append(resources, ResourceTypeManifest{
			ID: connSecretID,
			// readyWhen MUST be set (an ExternalSecret-only Resource isn't Ready by
			// default). We do NOT gate on the ExternalSecret's own Ready condition:
			// OC's `applied.<id>.status` snapshot does not reflect ESO's live status
			// for a foreign CRD (it reads empty/stale → the CEL strands the binding).
			// `${true}` makes the binding Ready once applied; the binding's outputs
			// still resolve the Secret, and ESO materialises it in ~1s — well before
			// the consumer (gated separately by the config-collection task) renders.
			ReadyWhen: "${true}",
			Template:  esTemplate,
		})
		for _, k := range secret {
			outputs = append(outputs, ResourceTypeOutput{
				Name:         k.Key,
				SecretKeyRef: &OCKeyRef{Name: "${metadata.name}-" + connSecretID, Key: k.Key},
			})
		}
	}

	return &ResourceType{
		APIVersion: ocAPIVersion,
		Kind:       kindResourceType,
		Metadata:   OCObjectMeta{Name: name},
		Spec: ResourceTypeSpec{
			EnvironmentConfigs: envConfigSchema,
			RetainPolicy:       retainPolicyDelete,
			Outputs:            outputs,
			Resources:          resources,
		},
	}, nil
}

func toRawTemplate(m map[string]any) (json.RawMessage, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("connection resourcetype: marshal template: %w", err)
	}
	return json.RawMessage(b), nil
}
