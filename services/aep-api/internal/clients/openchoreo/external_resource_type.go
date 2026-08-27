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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ExternalResourceRTTemplateVersion is bumped whenever BuildExternalResourceType's
// emitted manifest shape changes in a way that an already-applied ResourceType
// would NOT reflect. ResourceTypes are immutable AND shared by name across
// projects, and EnsureResourceType reuses an existing RT on 409-conflict — so
// without a version in the name, a stale RT authored by older code (e.g. v1's
// buggy `readyWhen` that gated on a foreign CRD's `.status.conditions` and
// threw "no such key") is silently reused. Pinning the version into the RT
// name makes a generator change author a fresh, correctly-shaped RT instead.
//
//	v1 → original (secret readyWhen gated on the ExternalSecret Ready condition; broken)
//	v2 → secret readyWhen = ${true} (an ES-only Resource isn't Ready by default; OC's
//	     applied.<id>.status snapshot is stale for foreign CRDs)
const ExternalResourceRTTemplateVersion = 2

// rtTemplateVersionLabel records the generator version on the RT for debugging.
// It uses the platform's own `aep.wso2.com/*` domain (see markers.go / ADR-0007),
// keeping the external RT symmetric with the platform-resource ClusterResourceType
// markers and clear of OC's `openchoreo.dev` domain.
const rtTemplateVersionLabel = "aep.wso2.com/rt-template-version"

// externalNameAnnotation / externalDescriptionAnnotation carry the external
// resource's logical identity on the authored RT — OC has no native
// description field, and the RT's own metadata.name is the hashed cluster
// name (see ExternalResourceRTName), not the logical one. They use the
// platform's `aep.wso2.com/*` domain (matching markers.go's
// aep.wso2.com/description and ADR-0007), so the external RT stays symmetric
// with the platform-resource markers and clear of OC's own `openchoreo.dev`
// annotations. ExternalDefinitionFromRT reads these back, plus optional
// consumption-instructions and resource-docs when present (read-only here;
// register writes them onto the ResourceType).
const (
	externalNameAnnotation            = "aep.wso2.com/external-name"
	externalDescriptionAnnotation     = "aep.wso2.com/description"
	consumptionInstructionsAnnotation = "aep.wso2.com/consumption-instructions"
	resourceDocsAnnotation            = "aep.wso2.com/resource-docs"
)

// ExternalResourceRTName is the cluster ResourceType name for an external
// resource: <name>-<schema hash>-t<templateVersion>. The hash is a short
// digest over the config schema's (key, secret) pairs only (see shortHash) —
// deliberately the same comparison repositories.SchemaEqual makes — so the
// SAME schema always yields the SAME name (a stable get-or-create target)
// while a changed key or secret flag mints a fresh name (ResourceTypes are
// effectively immutable, so a new shape needs a new one). A description or
// default-value-only edit does NOT change the name. The -t<version> suffix
// is unchanged from before: it pins the generator's template version so a
// generator change never collides with — and silently reuses — a stale RT
// of the same schema.
func ExternalResourceRTName(name string, keys []ExternalResourceConfigKey) string {
	return fmt.Sprintf("%s-%s-t%d", name, shortHash(keys), ExternalResourceRTTemplateVersion)
}

// shortHash is a short hex digest (first 10 hex chars of a sha256) over a
// config schema's (key, secret) pairs, sorted by key for order-independence.
// It deliberately ignores Description/DefaultValue — those can be edited
// freely without minting a new ResourceType — mirroring exactly what
// repositories.SchemaEqual compares.
func shortHash(keys []ExternalResourceConfigKey) string {
	type kv struct {
		key    string
		secret bool
	}
	pairs := make([]kv, len(keys))
	for i, k := range keys {
		pairs[i] = kv{k.Key, k.Secret}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].key < pairs[j].key })

	var b strings.Builder
	for _, p := range pairs {
		fmt.Fprintf(&b, "%s=%t;", p.key, p.secret)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])[:10]
}

// ExternalResourceConfigKey is one env-var key in an external resource's
// schema. Mirrors the agents/BFF spec.ConfigKey without importing models
// (keeps the OC client leaf-level).
type ExternalResourceConfigKey struct {
	Key    string
	Secret bool
	// Description is an optional human-readable note on what this value is
	// for. Carried into the authored RT's spec.parameters (never affects the
	// RT name — see shortHash) so ExternalDefinitionFromRT can recover it.
	Description string
	// DefaultValue is an optional suggested initial value for a non-secret
	// key. Carried into spec.parameters like Description. Never set for a
	// secret key — a credential has no default to invent.
	DefaultValue string
}

// Per-env value field names the BFF supplies on the binding's
// resourceTypeEnvironmentConfigs (and the ResourceType's environmentConfigs
// schema). Plain keys are supplied verbatim by their own key name; all secret
// values live in a single SM-API secret addressed by SecretStorePathField,
// read per-property by the ExternalSecret.
const (
	// SecretStorePathField is the environmentConfig holding the SM-API/OpenBao
	// KV path where this external resource's secret values live for the
	// environment.
	SecretStorePathField = "secretStorePath"
	// extResourceConfigMapID / extResourceSecretID are the ResourceType
	// manifest ids.
	extResourceConfigMapID = "config"
	extResourceSecretID    = "secret"
)

// retainPolicyDelete is the ResourceType/binding retainPolicy that cascades
// the rendered DP objects (ConfigMap/ExternalSecret) on delete.
const retainPolicyDelete = "Delete"

// BuildExternalResourceType turns an external resource's config key schema
// into a per-resource ResourceType (the "external SaaS dependency pattern" —
// no upstream sample exists, so this is modeled on the shipped `postgres`
// ResourceType + the `${dataplane.secretStore}` ExternalSecret form).
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
// `name` is the external resource's logical name (e.g. "salesforce") — it
// becomes the aep.wso2.com/external-name annotation, and (combined with
// the schema) the cluster RT name via ExternalResourceRTName. `description`
// (optional) becomes the aep.wso2.com/description annotation.
// `consumptionInstructions` and `resourceDocs` (URL/path pointers only — no
// spec bodies) are written to aep.wso2.com/consumption-instructions and
// aep.wso2.com/resource-docs when non-empty; empty values omit those
// annotations. The RT is self-describing: ExternalDefinitionFromRT
// reconstructs {name, description, config[], consumption, docs} from an
// authored RT without a DB round-trip. ResourceTypes are effectively
// immutable — a changed key/secret schema mints a new RT name
// (see ExternalResourceRTName); a description/default-only edit does not.
func BuildExternalResourceType(name, description string, keys []ExternalResourceConfigKey, consumptionInstructions string, resourceDocs []ResourceDoc) (*ResourceType, error) {
	if name == "" {
		return nil, fmt.Errorf("external resourcetype: empty name")
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("external resourcetype %q: at least one config key required", name)
	}

	var plain, secret []ExternalResourceConfigKey
	for _, k := range keys {
		if k.Key == "" {
			return nil, fmt.Errorf("external resourcetype %q: empty config key", name)
		}
		if k.Secret {
			secret = append(secret, k)
		} else {
			plain = append(plain, k)
		}
	}

	// spec.parameters: the self-describing schema — EVERY key (plain + secret
	// alike), carrying its description/default when present. This is the
	// discovery-facing schema ExternalDefinitionFromRT reads back; nothing in
	// the rendered templates references ${parameters.*} (per-env values flow
	// through environmentConfigs below), so it never gates a Resource — it is
	// documentation, mirroring how ClusterResourceTypes expose their
	// architect-facing schema (see platform_catalog.go).
	paramProps := make(map[string]any, len(keys))
	for _, k := range keys {
		prop := map[string]any{"type": "string"}
		if k.Description != "" {
			prop["description"] = k.Description
		}
		if k.DefaultValue != "" {
			prop["default"] = k.DefaultValue
		}
		paramProps[k.Key] = prop
	}
	paramSchema := &SchemaSection{OpenAPIV3Schema: map[string]any{
		"type":       "object",
		"properties": paramProps,
	}}

	// environmentConfigs schema: each plain key (string) + the secret store path
	// (string) when there are secrets. These are the per-env values the BFF
	// supplies on the binding's resourceTypeEnvironmentConfigs. Type-only (no
	// description/default) — it is the runtime binding-value schema OC
	// validates against, distinct from the discovery-facing spec.parameters.
	envProps := map[string]any{}
	for _, k := range plain {
		envProps[k.Key] = map[string]any{"type": "string"}
	}
	if len(secret) > 0 {
		envProps[SecretStorePathField] = map[string]any{"type": "string"}
	}
	envConfigSchema := &SchemaSection{OpenAPIV3Schema: map[string]any{
		"type":       "object",
		"properties": envProps,
	}}

	// ── ConfigMap (always) — non-secret values. ─────────────────────────────
	cmData := map[string]any{}
	for _, k := range plain {
		cmData[k.Key] = "${environmentConfigs." + k.Key + "}"
	}
	if len(cmData) == 0 {
		// Keep the ConfigMap non-empty when the resource is all-secret.
		cmData["_ready"] = "true"
	}
	cmTemplate, err := toRawTemplate(map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      "${metadata.name}-" + extResourceConfigMapID,
			"namespace": "${metadata.namespace}",
			"labels":    "${metadata.labels}",
		},
		"data": cmData,
	})
	if err != nil {
		return nil, err
	}
	resources := []ResourceTypeManifest{{
		ID:        extResourceConfigMapID,
		ReadyWhen: "${true}", // an applied ConfigMap is ready immediately
		Template:  cmTemplate,
	}}

	outputs := make([]ResourceTypeOutput, 0, len(keys))
	for _, k := range plain {
		outputs = append(outputs, ResourceTypeOutput{
			Name:            k.Key,
			ConfigMapKeyRef: &OCKeyRef{Name: "${metadata.name}-" + extResourceConfigMapID, Key: k.Key},
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
				"name":      "${metadata.name}-" + extResourceSecretID,
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
					"name":           "${metadata.name}-" + extResourceSecretID,
					"creationPolicy": "Owner",
				},
				"data": esData,
			},
		})
		if terr != nil {
			return nil, terr
		}
		resources = append(resources, ResourceTypeManifest{
			ID: extResourceSecretID,
			// readyWhen MUST be set (an ExternalSecret-only Resource isn't Ready by
			// default). We do NOT gate on the ExternalSecret's own Ready condition:
			// OC's `applied.<id>.status` snapshot does not reflect ESO's live status
			// for a foreign CRD (it reads empty/stale → the CEL strands the binding).
			// `${true}` makes the binding Ready once applied. This is deliberate even
			// while secretStorePath is empty: external value readiness is the AEP
			// `configured` state, not the binding's Ready condition, and builds must
			// not wait for credentials. ESO materialises the Secret after values are saved.
			ReadyWhen: "${true}",
			Template:  esTemplate,
		})
		for _, k := range secret {
			outputs = append(outputs, ResourceTypeOutput{
				Name:         k.Key,
				SecretKeyRef: &OCKeyRef{Name: "${metadata.name}-" + extResourceSecretID, Key: k.Key},
			})
		}
	}

	annotations := map[string]string{externalNameAnnotation: name}
	if description != "" {
		annotations[externalDescriptionAnnotation] = description
	}
	if consumptionInstructions != "" {
		annotations[consumptionInstructionsAnnotation] = consumptionInstructions
	}
	if len(resourceDocs) > 0 {
		raw, jerr := json.Marshal(resourceDocs)
		if jerr != nil {
			return nil, fmt.Errorf("external resourcetype %q: marshal resource-docs: %w", name, jerr)
		}
		annotations[resourceDocsAnnotation] = string(raw)
	}

	return &ResourceType{
		APIVersion: ocResourceAPIVersion,
		Kind:       kindResourceType,
		Metadata: OCObjectMeta{
			Name:        ExternalResourceRTName(name, keys),
			Labels:      map[string]string{rtTemplateVersionLabel: fmt.Sprintf("%d", ExternalResourceRTTemplateVersion)},
			Annotations: annotations,
		},
		Spec: ResourceTypeSpec{
			Parameters:         paramSchema,
			EnvironmentConfigs: envConfigSchema,
			RetainPolicy:       retainPolicyDelete,
			Outputs:            outputs,
			Resources:          resources,
		},
	}, nil
}

// ExternalResourceDefinition is the reconstruction of an authored external
// RT's definition — the inverse of BuildExternalResourceType.
//
// ConsumptionInstructions and ResourceDocs are read from RT annotations when
// present. Org value-plane cells and instances are not part of this type:
// ExternalDefinitionFromRT never invents them (production has no org value
// store). The provisioning catalog view fills those from CatalogValuePlane.
type ExternalResourceDefinition struct {
	Name                    string
	Description             string
	Config                  []ExternalResourceConfigKey
	ConsumptionInstructions string
	ResourceDocs            []ResourceDoc
}

// ResourceDoc is an org resource-docs pointer (type + URL or repo path),
// reconstructed from the aep.wso2.com/resource-docs annotation.
type ResourceDoc struct {
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
	Path string `json:"path,omitempty"`
}

// resourceDocTypes is the OpenAPI ResourceDocPointerDTO.type enum. Unknown
// annotation values must not reach the list DTO.
var resourceDocTypes = map[string]struct{}{
	"documentation": {},
	"openapi":       {},
	"graphql":       {},
	"asyncapi":      {},
	"protobuf":      {},
}

func parseResourceDocs(raw string) []ResourceDoc {
	var docs []ResourceDoc
	if err := json.Unmarshal([]byte(raw), &docs); err != nil {
		return nil
	}
	out := make([]ResourceDoc, 0, len(docs))
	for _, d := range docs {
		if _, ok := resourceDocTypes[d.Type]; ok {
			out = append(out, d)
		}
	}
	return out
}

// ExternalDefinitionFromRT recovers the external resource definition an
// authored RT carries: the logical name + description off the
// aep.wso2.com/external-name / aep.wso2.com/description
// annotations, consumption instructions and resource-docs pointers when
// those annotations are present, and each config key's key/description/default
// off spec.parameters, with secret classification taken from spec.outputs (a
// key is secret iff its output carries a secretKeyRef, plain iff a
// configMapKeyRef). Config is sorted by key for a deterministic result. ok is
// false when rt does not carry enough self-describing metadata to
// reconstruct (nil, no external-name annotation, or no spec.parameters
// properties) — e.g. an RT authored by pre-self-describing code.
//
// It does not invent env cells: those live on the provisioning catalog view
// via CatalogValuePlane, not on this reconstruction type.
func ExternalDefinitionFromRT(rt *ResourceType) (def ExternalResourceDefinition, ok bool) {
	if rt == nil {
		return ExternalResourceDefinition{}, false
	}
	name := rt.Metadata.Annotations[externalNameAnnotation]
	if name == "" {
		return ExternalResourceDefinition{}, false
	}

	var props map[string]any
	if rt.Spec.Parameters != nil {
		props, _ = rt.Spec.Parameters.OpenAPIV3Schema["properties"].(map[string]any)
	}
	if len(props) == 0 {
		return ExternalResourceDefinition{}, false
	}

	secretByKey := make(map[string]bool, len(rt.Spec.Outputs))
	for _, o := range rt.Spec.Outputs {
		switch {
		case o.SecretKeyRef != nil:
			secretByKey[o.Name] = true
		case o.ConfigMapKeyRef != nil:
			secretByKey[o.Name] = false
		}
	}

	config := make([]ExternalResourceConfigKey, 0, len(props))
	for key, raw := range props {
		prop, _ := raw.(map[string]any)
		k := ExternalResourceConfigKey{Key: key, Secret: secretByKey[key]}
		if d, ok := prop["description"].(string); ok {
			k.Description = d
		}
		if dv, ok := prop["default"].(string); ok {
			k.DefaultValue = dv
		}
		config = append(config, k)
	}
	sort.Slice(config, func(i, j int) bool { return config[i].Key < config[j].Key })

	var docs []ResourceDoc
	if raw := rt.Metadata.Annotations[resourceDocsAnnotation]; raw != "" {
		docs = parseResourceDocs(raw)
	}

	return ExternalResourceDefinition{
		Name:                    name,
		Description:             rt.Metadata.Annotations[externalDescriptionAnnotation],
		Config:                  config,
		ConsumptionInstructions: rt.Metadata.Annotations[consumptionInstructionsAnnotation],
		ResourceDocs:            docs,
	}, true
}

func toRawTemplate(m map[string]any) (json.RawMessage, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("external resourcetype: marshal template: %w", err)
	}
	return json.RawMessage(b), nil
}
