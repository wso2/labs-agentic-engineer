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

	"github.com/wso2/aep/aectl/internal/oidc"
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

	// The domains an install gets when it configures nothing: the k3d
	// convention deployments/scripts/setup-env-for-aectl.sh builds. Both are
	// suffixes, not whole hostnames — see Config.IDPBaseDomain.
	defaultIDPBaseDomain     = "openchoreo.localhost"
	defaultGatewayBaseDomain = "gateway.localhost"

	// The ports each plane's gateway serves on, plain and TLS. They move with
	// the scheme rather than independently: a cluster does not serve https on
	// 8080, so one flag picks the pair.
	idpPlainPort     = 8080
	idpTLSPort       = 8443
	gatewayPlainPort = 19080
	gatewayTLSPort   = 19443
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
	// issuer is configured from PlatformIDP's JWKS endpoint.
	PlatformThunderURL string

	// PlatformIDP is T1 as its discovery document names it (the public
	// issuer and endpoints). T2 trusts the issuer over HTTPS JWKS (ThunderID
	// refuses a plain-http trusted-issuer JWKS URL), reached through the
	// control-plane gateway on 8443. Reaching that URL over HTTPS from inside
	// the cluster (custom CA trust, DNS) is an environment prerequisite this
	// package does not configure — see the package-level Install doc comment.
	PlatformIDP oidc.Endpoints

	// IDPBaseDomain is the suffix this environment's IdP is published under:
	// the hostname is "<Env>-idp.<IDPBaseDomain>". Empty takes
	// defaultIDPBaseDomain.
	//
	// It is browser-facing, and not only to operators. thunder-app-operator
	// republishes the PublicURL built from it as every generated app's OIDC
	// issuer (binding.go's record → its own binding.go → the app's `issuer`
	// and `jwks_url` outputs), so an end user signing in to an app AEP
	// generated is redirected here. On any cluster reachable by more than
	// its own host, a value that resolves only to loopback breaks that
	// sign-in — which is why this is configuration and not a constant.
	//
	// Agent Manager reaches the same instance by COMPOSING this same suffix
	// with the environment's IdP handle rather than being told an address
	// (its ENV_IDP_BASE_DOMAIN, deployments/scripts/setup-agent-manager.sh).
	// The two must carry the same string or the products stop agreeing on
	// agent identity; keeping this a bare suffix rather than a URL is what
	// makes them copy-paste identical.
	IDPBaseDomain string

	// GatewayBaseDomain is the suffix this environment's API Platform gateway
	// answers on: the vhost is "<Env>-<Org>.<GatewayBaseDomain>". Empty takes
	// defaultGatewayBaseDomain. Separate from IDPBaseDomain because the two
	// share only their trailing labels — "gateway." and "openchoreo." are
	// different parents, and a single key could only express both by baking
	// those labels in as structure.
	//
	// Also browser-facing: it fronts the APIs a generated app calls.
	GatewayBaseDomain string

	// TLS is whether this cluster's public endpoints are served over HTTPS,
	// and it moves the scheme and both gateway ports together.
	//
	// Not a preference. A browser exposes crypto.subtle only in a secure
	// context, and a generated app's OIDC login needs it for PKCE — so on any
	// domain other than localhost, end users cannot sign in to a generated app
	// at all without this. (*.localhost is a secure context by definition,
	// which is why the local flow never needed it.)
	//
	// Must match WITH_TLS in deployments/scripts/setup-env-for-aectl.sh: that
	// script stamps the platform IdP's issuer, and a T2 that advertises a
	// different scheme than the cluster serves is the same class of mismatch
	// as a wrong domain.
	TLS bool

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

// idpBaseDomain and gatewayBaseDomain resolve the two suffixes, so the
// fallback to the k3d convention lives in one place rather than at each call
// site — a helper reached directly by a test gets the same answer Install
// would give it.
func (c Config) idpBaseDomain() string {
	if c.IDPBaseDomain != "" {
		return c.IDPBaseDomain
	}
	return defaultIDPBaseDomain
}

func (c Config) gatewayBaseDomain() string {
	if c.GatewayBaseDomain != "" {
		return c.GatewayBaseDomain
	}
	return defaultGatewayBaseDomain
}

// scheme and the port helpers resolve the pair TLS selects, so no call site
// has to remember that https goes with 8443 and http with 8080.
func (c Config) scheme() string {
	if c.TLS {
		return "https"
	}
	return "http"
}

func (c Config) idpPort() int {
	if c.TLS {
		return idpTLSPort
	}
	return idpPlainPort
}

func (c Config) gatewayPort() int {
	if c.TLS {
		return gatewayTLSPort
	}
	return gatewayPlainPort
}

// publicURL is the URL this package exposes T2 on: an httproute on the same
// shared control-plane gateway and domain the platform IdP (T1) already uses
// (see deployments/scripts/setup-env-for-aectl.sh's
// "thunder.openchoreo.<domain>" convention for T1), rather than Agent
// Manager's own "<env>-idp.amp.localhost" — this package has no Agent
// Manager routing to depend on.
//
// Scheme and port come from Config.TLS, because they are what a browser
// judges: this URL becomes every generated app's OIDC issuer, and a PKCE
// login against a plain-http issuer cannot complete outside localhost.
func publicURL(env, baseDomain, scheme string, port int) string {
	return fmt.Sprintf("%s://%s-idp.%s:%d", scheme, env, baseDomain, port)
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
