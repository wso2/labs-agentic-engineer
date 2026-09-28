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

// Package envidp installs an environment's own identity provider (the
// "second tier" or "T2" Thunder — see deployments/design/two-tier-thunder.md)
// and its API Platform gateway, and writes the binding record that lets
// thunder-app-operator and aep-api find it.
//
// aectl only ever provisions into one (org, environment) pair — the same
// Environment cmd.ocPipelineSourceEnvironment() addresses, in the namespace
// cmd.ocOrgNamespace() names. Both default to "default" but follow config
// (oc.pipeline_source_environment and oc.default_org_namespace
// respectively); this package's Org/Env must resolve the same way
// platform_gateway.go's checkGatewayIngress/applyGatewayIngressConfig do, or
// it would target a different Environment object than the one those
// functions actually read and patch. This package takes both as input rather
// than importing either, so cmd remains the single place that decides which
// (org, environment) AEP installs into.
//
// Deliberately independent of Agent Manager at runtime: it never calls
// amp-api, never mirrors Agent Manager's Helm values shape, and derives
// names from a fixed convention instead of Agent Manager's own naming
// library (deployments/scripts/setup-environment-thunder.sh sources that
// library from a separate Agent Manager checkout — not something a portable
// aectl binary can depend on). For Org="default" this convention produces
// the same name that library would for a short, unhashed input; see
// Config's doc comment for the collision risk a longer Org or Env leaves.
package envidp

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
)

const (
	thunderChart        = "oci://ghcr.io/thunder-id/helm-charts/thunderid"
	thunderChartVersion = "1.0.0"
	thunderAdminPort    = 8090

	// gatewayChart is Agent Manager's own chart — the actual mechanism
	// deployments/scripts/setup-environment-gateway.sh uses, confirmed to
	// install and serve correctly with no Agent Manager instance running
	// (its own script only skips the AMP *registration* step, "bootstrap",
	// when amp-api doesn't answer; the chart install itself is unconditional).
	// This remains a supply-chain dependency on an Agent-Manager-published
	// artifact — not a runtime dependency on Agent Manager being installed.
	gatewayChart        = "oci://ghcr.io/wso2/wso2-amp-api-platform-gateway-extension"
	gatewayChartVersion = "1.0.0-rc2"

	// operatorNamespace is thunder-app-operator's own namespace — where its
	// Secret informer and RBAC are restricted to (see its own binding.go),
	// so the binding Secret must be mirrored there by name.
	operatorNamespace = "thunder-app-operator-system"
)

// Config parameterizes Install. Org and Env are explicit fields, not
// hardcoded constants, so a caller — and this package's tests — never has to
// guess which literal is being depended on. In practice the caller (cmd)
// passes Org=cmd.ocOrgNamespace() (the Environment's namespace, configurable
// via oc.default_org_namespace) and Env=cmd.ocPipelineSourceEnvironment()
// (the Environment's name, configurable via oc.pipeline_source_environment)
// — see the package doc comment for why those are NOT the same axis despite
// sharing a literal by default.
type Config struct {
	// Org, Env identify the (organization, environment) pair this instance
	// serves. Release, namespace, and binding-record names are all derived
	// from these two — see releaseName. Agent Manager derives the same names
	// through its own naming library (thunder-naming.sh, staged from a
	// separate checkout); this package does not depend on that library, so
	// for any pair other than the short, unhashed "default"/"default" this
	// codebase uses today, the derived name below is NOT guaranteed to match
	// what Agent Manager would compute for the same pair — installing this
	// package's T2 alongside a later Agent Manager environment for a longer
	// (org, env) name risks two divergent Thunder releases for one
	// environment. Documented in deployments/design/two-tier-thunder.md as
	// an existing risk of not vendoring that library, not one this package
	// introduces. If Env ever varies from "default" in practice, this risk
	// applies to it now too, not just to a hypothetical longer name.
	Org, Env string

	// PlatformThunderURL is the platform IdP's (T1) in-cluster admin URL —
	// used only to log which issuer T2 will trust; T2's trust of T1 as an
	// issuer is configured via PlatformThunderPublicURL's JWKS endpoint.
	PlatformThunderURL string

	// PlatformThunderPublicURL is T1's public URL. T2 trusts it as an issuer
	// over HTTPS JWKS (ThunderID refuses a plain-http trusted-issuer JWKS
	// URL). Reaching that URL over HTTPS from inside the cluster (custom
	// CA trust, DNS) is an environment prerequisite this package does not
	// configure — see the package-level Install doc comment.
	PlatformThunderPublicURL string

	// Kubeconfig is forwarded to every kubectl/helm invocation; empty uses
	// the default (~/.kube/config or in-cluster).
	Kubeconfig string

	// OpenBaoNamespace, OpenBaoRelease, OpenBaoServiceAccount, and
	// OpenBaoWriteRole address the same OpenBao instance
	// cmd.provisionOpenBao already writes T1's secrets into — passed in
	// rather than re-declared here so cmd's own constants
	// (ocOpenBaoNamespace/ocOpenBaoRelease/ocOpenBaoSA/ocWriteRole) stay the
	// single source of truth for them.
	OpenBaoNamespace      string
	OpenBaoRelease        string
	OpenBaoServiceAccount string
	OpenBaoWriteRole      string
}

// maxReleaseName is Helm's own release-name limit. releaseName's result also
// serves as the namespace name (installThunder sets namespace := release),
// which Kubernetes bounds at 63 — the tighter Helm limit governs.
const maxReleaseName = 53

// releaseName is this package's fixed naming convention: both the Helm
// release and its namespace, for a (org, env) pair. See Config's doc comment
// for why this is not Agent Manager's own naming-library output in general.
//
// An (org, env) pair short enough to fit is returned verbatim — every
// existing install (e.g. "thunder-default-development") is unaffected by the
// bound below. Only a pair long enough to push the natural name past
// maxReleaseName gets truncated, with a stable hash of the FULL natural name
// appended so two long names that happen to share a prefix past the
// truncation point don't collide on the same release/namespace.
func releaseName(org, env string) string {
	natural := fmt.Sprintf("thunder-%s-%s", org, env)
	if len(natural) <= maxReleaseName {
		return natural
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(natural))
	suffix := fmt.Sprintf("-%08x", h.Sum32()) // 9 chars: '-' + 8 hex
	head := strings.TrimRight(natural[:maxReleaseName-len(suffix)], "-")
	return head + suffix
}

// publicURL is the hostname this package exposes T2 on: an httproute on the
// same shared data-plane gateway and domain the platform IdP (T1) already
// uses (see deployments/scripts/setup-env-for-aectl.sh's
// "thunder.openchoreo.localhost" convention for T1), rather than Agent
// Manager's own "<env>-idp.amp.localhost" — this package has no Agent
// Manager routing to depend on.
func publicURL(env string) string {
	return fmt.Sprintf("http://%s-idp.openchoreo.localhost:8080", env)
}

// validReleaseName rejects a releaseName result that is not a legal Helm
// release name — installThunder also uses it verbatim as a Kubernetes
// namespace, so it must satisfy the same DNS-1123 label rules a namespace
// name does. releaseName only bounds length; it does not touch the character
// set, so a Config.Org/Env sourced from free-form config (see Config's doc
// comment — neither is validated against a live k8s object before reaching
// here) can still produce an invalid name, e.g. from uppercase or
// underscores. Caught here, once, before Install does anything to the
// cluster, instead of surfacing as a Helm/namespace-create API error deep
// inside installThunder.
func validReleaseName(name string) error {
	if errs := validation.IsDNS1123Label(name); len(errs) > 0 {
		return fmt.Errorf("derived name %q is not a valid Helm release/namespace name: %s", name, strings.Join(errs, "; "))
	}
	return nil
}

// adminURL is T2's in-cluster Service address — what this package and
// thunder-app-operator reach it at from inside the cluster.
func adminURL(release, namespace string) string {
	return fmt.Sprintf("http://%s-service.%s.svc.cluster.local:%d", release, namespace, thunderAdminPort)
}

// clients bundles the external clients Install's steps need, so each step
// function takes one argument instead of a growing parameter list, and so
// tests can substitute a fake Kubernetes clientset without touching the
// filesystem/network at all.
type clients struct {
	k8s        kubernetes.Interface
	kubeconfig string
}

func (c clients) applyKubectl(ctx context.Context, args ...string) ([]byte, error) {
	return execKubectl(ctx, c.kubeconfig, args...)
}
