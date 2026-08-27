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

// Package ocname is the OpenChoreo naming vocabulary: the Resource and binding
// names AEP derives for a project's external and platform resources, the scoped
// component name OC identifies a component by, and the env-var names a consumer
// reads their outputs from.
//
// It lives in the kernel rather than in the dependencies domain because TWO
// domains derive the same names and must agree byte-for-byte: dependencies
// authors the resources and injects their outputs as pod env vars, and spec
// stamps the very same ref + env-var names into design.json at design save so the
// coding agent can author its workload.yaml (see spec/derive_wiring.go). spec
// cannot import dependencies — dependencies already imports spec — so a shared
// home is the only way to keep one source of truth instead of two conventions
// that drift.
package ocname

import (
	"fmt"
	"hash/fnv"
	"strings"
)

const (
	// cnpgMaxClusterName is CloudNativePG's hard cap on a Cluster metadata.name
	// (kubectl-verified: "the maximum length of a cluster name is 50 characters").
	// It is the strictest length limit any platform-resource backing store
	// imposes, so it governs the bound for the OC Resource name below.
	cnpgMaxClusterName = 50
	// maxEnvNameLen is the longest environment slug AEP embeds in a render name.
	// "development" (openchoreo.DevEnvironmentName, 11 chars) is the only — and longest
	// — environment v1 provisions into; "production"/"staging" are shorter.
	maxEnvNameLen = 11
	// ocRenderDecoration is the overhead OpenChoreo adds when it renders a Resource
	// into a per-env backing object. LIVE-VERIFIED on OC 1.1.1: the rendered object
	// (e.g. the CloudNativePG Cluster) is named off the RESOURCE name, not the
	// binding — r-<resourceName>-<env>-<hash8> — so the decoration is
	// "r-" (2) + "-" + env + "-" + an 8-char hash, plus one guard char for a wider
	// hash. (The earlier #165 bound assumed r-<bindingName>-<hash> and bounded the
	// binding name; that never governed the Cluster name, which overflowed at 52.)
	ocRenderDecoration = 2 + 1 + maxEnvNameLen + 1 + 8 + 1 // 24
	// maxOCResourceName is the longest a Resource metadata.name may be so its
	// OC-rendered r-<name>-<env>-<hash> backing object stays within
	// cnpgMaxClusterName. The render root is the RESOURCE name, so this bound lives
	// on ExternalResourceName. Longer names are hash-truncated by boundName.
	maxOCResourceName = cnpgMaxClusterName - ocRenderDecoration // 26
	// maxOCBindingName keeps a binding metadata.name a sane, legal DNS-1035 label.
	// A binding is not a render root, and since the Resource name it derives from
	// is already bounded to maxOCResourceName, a binding stays within this by
	// construction; the bound is a defensive guard, not the CNPG-governing one.
	maxOCBindingName = maxOCResourceName + 1 + maxEnvNameLen // 38
)

// boundName returns natural unchanged when it already fits max; otherwise it
// replaces the overflowing tail with a deterministic 8-hex FNV-1a hash of the
// FULL natural name. The hash makes collisions between distinct long names
// negligible (a 32-bit space against per-org resource cardinality) rather than
// the near-certain prefix collision a plain truncation would cause, while the
// result stays a valid DNS-1035 label (lowercase, starts with a letter, no
// trailing '-') within max. Short names are returned byte-for-byte, so existing
// bindings keep their readable names and only overflowing names change — no
// migration of already provisioned resources is needed.
func boundName(natural string, max int) string {
	if len(natural) <= max {
		return natural
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(natural))
	suffix := fmt.Sprintf("-%08x", h.Sum32()) // 9 chars: '-' + 8 hex
	head := strings.TrimRight(natural[:max-len(suffix)], "-")
	return head + suffix
}

// EnvVarName builds a valid C_IDENTIFIER env-var name from a dependency name +
// output name (join with "_", map every char outside [A-Za-z0-9_] to '_',
// upper-case). It is the SINGLE source of truth for the platform-resource
// output naming convention: the provisioning wiring (pod env-var injection in
// wiring.go) and the SPA runtime config (window._env_ keys in runtimeconfig)
// both derive their keys through it, so the coding agent and the browser see
// byte-identical names. e.g. "orders-db" + "host" → "ORDERS_DB_HOST";
// "user-auth" + "client_id" → "USER_AUTH_CLIENT_ID".
func EnvVarName(depName, outName string) string {
	joined := depName + "_" + outName
	mapped := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			return r
		default:
			return '_'
		}
	}, joined)
	return strings.ToUpper(mapped)
}

// ScopedComponentName is the k8s metadata name OC uses for a component. OC
// components across every project in an org share a single k8s namespace, so two
// projects cannot hold the same component name unless we disambiguate; the
// project prefixes the name, and the user's original name survives as the
// display-name annotation.
//
// It lives here, beside EnvVarName, for the same reason: THREE call sites derive
// this name and must agree byte-for-byte — the OC client at the API boundary,
// provisioning when it resolves a sibling's endpoint dependency, and spec when it
// stamps that sibling's `component` into design.json at design save. The third is
// what makes the shared home necessary: a sibling endpoint dependency that names
// the FRIENDLY component resolves to nothing in OpenChoreo (the binding lookup is
// by scoped name), so the consumer's ReleaseBinding never reaches Ready and the
// platform reports "deploying" forever.
//
// Callers must always pass the friendly component name (never an already-scoped
// name) — scope exactly once.
func ScopedComponentName(projectName, componentName string) string {
	if projectName == "" {
		return componentName
	}
	return projectName + "-" + componentName
}

// ServiceURLEnvName is the env var a consumer reads a provider component's
// resolved base URL from — the `envBindings.address` value on a workload
// `dependencies.endpoints[]` entry. Keyed on the LOGICAL dependency name (never
// the scoped component name), so the env var the coding agent codes against does
// not change when the OC scoping rule does: "todo-api" → "TODO_API_URL".
//
// Shared for the same byte-for-byte reason as EnvVarName: provisioning binds this
// name into the pod env, spec stamps it into design.json, and runtimeconfig emits
// it as a window._env_ key for a browser app.
func ServiceURLEnvName(depName string) string {
	return EnvVarName(depName, "URL")
}

// ServiceGatewayURLEnvName is the env var a consumer reads a PROTECTED provider
// component's gateway address from: "todo-api" → "TODO_API_GATEWAY_URL". It sits
// beside ServiceURLEnvName because the two name the same provider reached two
// different ways, and the difference is authentication:
//
//   - ServiceURLEnvName is the project Service, reached directly. Nothing
//     validates a token and nothing injects identity headers.
//   - This one is the API gateway, which validates the caller's bearer token and
//     injects X-User-* from its claims. The platform emits it only for a provider
//     whose design declares `exposesAPI.auth` (spec.ResolveAPISecurityEnabled).
//
// A consumer that carries untrusted traffic — a SPA's nginx proxying the
// browser's /api — must prefer this one. Keyed on the LOGICAL dependency name for
// the same reason as ServiceURLEnvName: the var the coding agent codes against
// does not move when OC's scoping rule does.
func ServiceGatewayURLEnvName(depName string) string {
	return EnvVarName(depName, "GATEWAY_URL")
}

// ExternalResourceName is the per-project OC Resource name (== the Workload
// dependency `ref`) for a project's external OR platform resource. metadata.name
// is namespace-unique — owner.projectName does NOT scope it — so the project
// prefixes the name. It is the render root: OC names a platform resource's
// backing object r-<thisName>-<env>-<hash>, so the name is bounded to
// maxOCResourceName to keep a CloudNativePG Cluster within its 50-char cap.
// boundName leaves already-short names byte-for-byte, so only overflowing names
// change (and an overflowing CNPG name never provisioned, so nothing to migrate).
// Exported: the Resource author, the binding owner ref, the consumer-dependency
// renderer, and deprovision all derive the same name through this single source
// of truth, so the bound stays consistent across every use.
func ExternalResourceName(project, name string) string {
	return boundName(project+"-"+name, maxOCResourceName)
}

// ExternalResourceBindingName is the per-env ResourceReleaseBinding name an
// external OR platform resource's outputs are read from. It composes on the
// already-bounded ExternalResourceName, so it stays a legal, sane DNS-1035 label
// (maxOCBindingName) by construction. NOTE: the binding is NOT the CNPG render
// root — OC names the backing Cluster off the RESOURCE name (see
// ExternalResourceName), which is where the 50-char bound is enforced. Every
// read/write of a binding name routes through here, so provision, deprovision,
// status, and consumer wiring stay consistent.
func ExternalResourceBindingName(project, name, env string) string {
	return boundName(ExternalResourceName(project, name)+"-"+env, maxOCBindingName)
}
