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

package config

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/viper"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/wso2/aep/aectl/internal/idpclaims"
)

// ConfigMapName is the in-cluster ConfigMap written by `aectl init` and read by
// all subsequent commands. It lives in the AEP platform namespace (wso2-aep).
const ConfigMapName = "aep-cli-config"

// ThunderAdminCredsSecret is the ESO-synced Secret that holds the Thunder
// admin client credentials used by aectl to register OAuth clients.
const ThunderAdminCredsSecret = "aep-thunder-admin-creds"

// ThunderAdminCredsSecretKey is the key within ThunderAdminCredsSecret
// that holds the admin OAuth client secret.
const ThunderAdminCredsSecretKey = "client-secret"

// ConfigMapKeys is the canonical list of non-sensitive viper keys stored in
// ConfigMapName. thunder.admin_client_secret is intentionally absent — it is
// managed by OpenBao/ESO and read from the ThunderAdminCredsSecret instead.
var ConfigMapKeys = []string{
	"thunder.namespace",
	"thunder.url",
	"thunder.admin_client_id",
	"thunder.public_url",
	"idp.clients",
	"idp.claims.preset",
	"idp.claims.org.handle",
	"idp.claims.org.id",
	"idp.claims.org.name",
	"idp.claims.caller_type.claim",
	"idp.claims.caller_type.machine",
	"idp.claims.caller_type.user",
	"idp.issuer",
	"idp.org_handle",
	"console.public_url",
	"tryit.public_url",
	"oc.api_url",
	"oc.observability_api_url",
	"oc.system_namespace",
	"oc.default_org_namespace",
	"oc.pipeline_source_environment",
	"oc.local_org_provisioning.enabled",
	"oc.data_plane_gateway_tls",
	"codingagent.openbao_direct.enabled",
	"openbao.addr",
	"ae_studio.webhook_relay.enabled",
	"ae_studio.jev.enabled",
	"ae_studio.jev.url",
	"gateway.hostname",
	"environment.idp_base_domain",
	"environment.gateway_base_domain",
	"tls.enabled",
}

// keyKind describes the expected type of a config value for validation.
type keyKind int

const (
	kindString keyKind = iota
	kindBool
	kindURL
	kindEnum
)

// Who creates AE's clients on the platform IdP (idp.clients). ClientsThunder:
// aectl registers them on a ThunderID it administers. ClientsImported: the
// IdP's admin created them and imports their secrets; aectl administers
// nothing on the IdP.
const (
	ClientsThunder  = "thunder"
	ClientsImported = "imported"
)

type configKeyMeta struct {
	required   bool
	kind       keyKind
	enumValues []string // only for kindEnum
	// clients ties the key to one value of idp.clients: it is then required
	// under that value (whatever required says) and not required under
	// another. exclusive also refuses a value under another, for a key
	// nothing reads there.
	clients   string
	exclusive bool
}

// keyRegistry maps each ConfigMapKey to its validation metadata.
var keyRegistry = map[string]configKeyMeta{
	// Who creates AE's clients on the platform IdP. Optional; unset is
	// thunder.
	"idp.clients": {required: false, kind: kindEnum, enumValues: []string{ClientsImported, ClientsThunder}},
	// The ThunderID aectl administers: where it runs, how pods and the
	// browser reach it, and the admin client aectl registers clients with.
	// Required when aectl registers the clients (idp.clients thunder or
	// unset) and refused otherwise, where nothing reads them.
	"thunder.namespace":       {clients: ClientsThunder, exclusive: true, kind: kindString},
	"thunder.url":             {clients: ClientsThunder, exclusive: true, kind: kindURL},
	"thunder.admin_client_id": {clients: ClientsThunder, exclusive: true, kind: kindString},
	"thunder.public_url":      {clients: ClientsThunder, exclusive: true, kind: kindURL},
	// The claim names and values of the platform IdP: how its tokens name the
	// caller's org and caller type (internal/idpclaims). aectl passes them to
	// the chart. Every key is optional here: a value comes from the preset
	// (thunder when idp.claims.preset is unset), then from these keys.
	// validateViper requires the block to be complete after that layering.
	"idp.claims.preset":              {required: false, kind: kindEnum, enumValues: idpclaims.Presets()},
	"idp.claims.org.handle":          {kind: kindString},
	"idp.claims.org.id":              {kind: kindString},
	"idp.claims.org.name":            {kind: kindString},
	"idp.claims.caller_type.claim":   {kind: kindString},
	"idp.claims.caller_type.machine": {kind: kindString},
	"idp.claims.caller_type.user":    {kind: kindString},
	// The platform IdP's issuer: the iss of every token, and where the
	// discovery document is read. Required with imported clients, where it
	// must be https (see validateViper). Optional on a ThunderID aectl
	// administers, whose issuer is thunder.public_url when this is unset.
	"idp.issuer": {clients: ClientsImported, kind: kindURL},
	// The handle the IdP's tokens name the one org by: a ThunderID
	// organization unit's handle, an Asgardeo organization's handle. aep-api
	// maps it to oc.default_org_namespace, and on ThunderID aectl registers
	// the org's two clients under that organization unit.
	"idp.org_handle": {required: true, kind: kindString},
	// The platform's three browser-facing origins. All optional: each is bound
	// to a flag carrying the local default, so a file that omits them installs
	// the same cluster it always did. Set them together — re-domaining one and
	// not the others is the failure that looks like a working install, since
	// the console links to Try-it and aep-api registers Try-it's callback as a
	// redirect URI.
	"console.public_url": {required: false, kind: kindURL},
	"tryit.public_url":   {required: false, kind: kindURL},
	"oc.api_url":         {required: true, kind: kindURL},
	// In-cluster URL of the OpenChoreo Observer. Empty leaves the chart's own
	// default (see values.yaml's observer.baseURL) — coding-cycle log
	// archiving degrades gracefully when neither is reachable.
	"oc.observability_api_url": {required: false, kind: kindURL},
	"oc.system_namespace":      {required: true, kind: kindString},
	// The k8s namespace of the one org AEP ships with — AEP is single-org
	// today, so this is that default org's home namespace: where its Project,
	// Environment(s), DeploymentPipeline, and per-org ComponentTypes and
	// ProjectType (localOrgProvisioning) all live. Empty falls back to
	// "default" (see ocOrgNamespace in cmd/platform_gateway.go).
	"oc.default_org_namespace": {required: false, kind: kindString},
	// The OpenChoreo Environment aectl configures gateway ingress and the
	// environment Thunder on at install time: the root of the default org's
	// DeploymentPipeline/default, which is the write target aep-api resolves for
	// projects on that pipeline. aep-api does not read this. Empty falls back to
	// "default" (see ocPipelineSourceEnvironment in cmd/platform_gateway.go).
	"oc.pipeline_source_environment":    {required: false, kind: kindString},
	"oc.local_org_provisioning.enabled": {required: false, kind: kindBool},
	// Whether the data-plane gateway aectl is pointing at terminates TLS.
	// Unset (false) matches aectl's typical target — its own gateway setup
	// (envidp's Thunder+gateway install) always advertises plain http:// URLs
	// with no certificate-issuance step to wait for, so aep-api's
	// endpoint-reachability wait (gated on this exact flag) would otherwise
	// hold every web component at "converging" forever probing a URL that can
	// never answer. Set true only when aectl is installing against a gateway
	// that genuinely fronts TLS.
	"oc.data_plane_gateway_tls":          {required: false, kind: kindBool},
	"codingagent.openbao_direct.enabled": {required: false, kind: kindBool},
	"openbao.addr":                       {required: false, kind: kindURL},
	// The per-org AE Studio webhook relay (smee.io channel per org) for a
	// cluster GitHub cannot reach. Passed as aeStudio.webhookRelay.enabled on
	// install and update; absent is false. Never true in production.
	"ae_studio.webhook_relay.enabled": {required: false, kind: kindBool},
	// The platform's Jev key (issue classification): passed as
	// aeStudio.jev.enabled / aeStudio.jev.url on install and update, so a
	// re-install keeps it on. Absent is off; an empty url is Jev's default.
	// The key itself is a secret: `aectl platform secret import --path
	// aep/jev-api-key`.
	"ae_studio.jev.enabled": {required: false, kind: kindBool},
	"ae_studio.jev.url":     {required: false, kind: kindString},
	// gateway.hostname, when set, lets `aectl platform install` configure the
	// external gateway ingress non-interactively (CI-friendly path).
	"gateway.hostname": {required: false, kind: kindString},
	// The DNS suffixes the environment tier is published under:
	// "<env>-idp.<idp_base_domain>" for its identity provider and
	// "<env>-<org>.<gateway_base_domain>" for its API gateway. Bare domains,
	// not URLs — the scheme and port are fixed to this cluster's gateways.
	//
	// Both are browser-facing on any cluster reachable by more than its own
	// host: a generated app sends its END USERS to the first to sign in, and
	// calls its own API through the second. Empty takes the k3d convention
	// (openchoreo.localhost / gateway.localhost — see internal/envidp).
	//
	// idp_base_domain must equal Agent Manager's ENV_IDP_BASE_DOMAIN. Agent
	// Manager composes that origin rather than being told an address, so two
	// different values leave the products disagreeing on agent identity with
	// nothing failing at install time.
	"environment.idp_base_domain":     {required: false, kind: kindString},
	"environment.gateway_base_domain": {required: false, kind: kindString},
	// Whether this cluster's public endpoints are served over HTTPS. One
	// toggle, because the scheme and both gateway ports move together — a
	// cluster does not serve https on 8080.
	//
	// It decides whether an end user can sign in to a GENERATED APP at all:
	// browsers expose crypto.subtle only in a secure context, the OIDC login
	// needs it for PKCE, and the environment identity provider's URL is built
	// from this. Off is correct only on localhost, which browsers already
	// treat as secure.
	//
	// Must agree with WITH_TLS in deployments/scripts/setup-env-for-aectl.sh:
	// that script stamps the platform IdP's issuer and provisions the
	// certificate, and a disagreement is the same class of mismatch as a
	// wrong domain — it installs clean and fails at the first sign-in.
	"tls.enabled": {required: false, kind: kindBool},
}

// Init sets env-var bindings. All config values must come from the cluster
// ConfigMap loaded by LoadFromCluster — no hardcoded defaults are set here.
func Init() {
	viper.SetEnvPrefix("AEP")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()
}

// ValidateFile reads the YAML at path into an isolated viper instance and
// returns one error string per invalid or missing required key. Empty slice
// means the file is valid.
func ValidateFile(path string) []string {
	fv := viper.New()
	fv.SetConfigFile(path)
	if err := fv.ReadInConfig(); err != nil {
		return []string{fmt.Sprintf("cannot read config file: %s", err)}
	}
	return withClaimsEnvRefusal(validateViper(fv))
}

// ValidateLoaded checks the currently resolved global viper state (populated
// after LoadFromCluster) and returns one error string per invalid or missing
// required key.
func ValidateLoaded() []string {
	return withClaimsEnvRefusal(validateViper(viper.GetViper()))
}

// FileClaims resolves the idp.claims block of a config file: the preset it
// names (idp.claims.preset), then its own idp.claims.* keys.
func FileClaims(path string) (idpclaims.Resolved, error) {
	fv := viper.New()
	fv.SetConfigFile(path)
	if err := fv.ReadInConfig(); err != nil {
		return idpclaims.Resolved{}, fmt.Errorf("cannot read config file: %w", err)
	}
	return ResolveClaims(fv)
}

// ResolveClaims resolves the idp.claims block of v: the preset it names
// (idp.claims.preset, thunder when unset), then its own idp.claims.* keys.
func ResolveClaims(v *viper.Viper) (idpclaims.Resolved, error) {
	return idpclaims.Resolve(v.GetString("idp.claims.preset"), ClaimsOverride(v))
}

// claimsEnvPrefix is the prefix of the environment variables that the config
// reader maps onto the idp.claims.* keys, as it maps every AEP_* variable
// onto its key.
const claimsEnvPrefix = "AEP_IDP_CLAIMS_"

// claimsEnvSet lists the AEP_IDP_CLAIMS_* variables in environ, sorted.
func claimsEnvSet(environ []string) []string {
	var set []string
	for _, kv := range environ {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, claimsEnvPrefix) {
			set = append(set, name)
		}
	}
	sort.Strings(set)
	return set
}

// ClaimsEnvRefusal is the error for a claim value given in the environment,
// or nil when no AEP_IDP_CLAIMS_* variable is set. Such a value would hold
// for one run: the stored config keeps what the config file said, and the
// next update without the variable would send that value to the chart,
// where every reader would then read a claim the tokens do not carry. A
// claim value belongs in the config. Every command that validates a config
// stops on it first.
func ClaimsEnvRefusal() error {
	set := claimsEnvSet(os.Environ())
	if len(set) == 0 {
		return nil
	}
	return fmt.Errorf("%s: the platform IdP's claim values are not taken from the environment. Set them in the config (idp.claims.*), import it (aectl platform config import --config <file>), and unset the variable", strings.Join(set, ", "))
}

// withClaimsEnvRefusal puts ClaimsEnvRefusal first in errs: a variable that
// is set can cause the other errors, and it is the one to fix.
func withClaimsEnvRefusal(errs []string) []string {
	if err := ClaimsEnvRefusal(); err != nil {
		return append([]string{err.Error()}, errs...)
	}
	return errs
}

// ClaimsOverride answers the idp.claims.* values v sets itself, for layering
// over a preset (idpclaims.Resolve). An empty value sets nothing.
func ClaimsOverride(v *viper.Viper) idpclaims.Override {
	return func(key string) (string, idpclaims.Source) {
		return v.GetString("idp.claims." + key), idpclaims.SourceConfig
	}
}

// clientsOf is who creates the IdP's clients in v: idp.clients, thunder when
// unset. An unknown value is returned as it is; the enum check reports it,
// and the keys tied to a value are then all "another value's".
func clientsOf(v *viper.Viper) string {
	if c := v.GetString("idp.clients"); c != "" {
		return c
	}
	return ClientsThunder
}

func validateViper(v *viper.Viper) []string {
	var errs []string
	clients := clientsOf(v)
	// The IdP's claims have to be complete once the preset and the config's
	// own keys are layered. An unknown preset is the enum check's to report,
	// once.
	if _, err := ResolveClaims(v); err != nil && !errors.Is(err, idpclaims.ErrUnknownPreset) {
		errs = append(errs, err.Error())
	}
	// An IdP aectl does not run is reached over the public internet, by the
	// operator's machine and by every pod: its issuer has to be https.
	if issuer := v.GetString("idp.issuer"); clients == ClientsImported && issuer != "" && !strings.HasPrefix(issuer, "https://") {
		errs = append(errs, fmt.Sprintf("idp.issuer: must be an https:// URL when idp.clients is %s: %q", ClientsImported, issuer))
	}
	for _, k := range ConfigMapKeys {
		meta, ok := keyRegistry[k]
		if !ok {
			continue
		}
		val := v.GetString(k)
		required := meta.required
		if meta.clients != "" {
			required = meta.clients == clients
			if !required && meta.exclusive && val != "" {
				errs = append(errs, fmt.Sprintf("%s: read only when idp.clients is %s; unset it", k, meta.clients))
				continue
			}
		}
		if required && val == "" {
			if meta.clients != "" {
				errs = append(errs, fmt.Sprintf("%s: required when idp.clients is %s but not set", k, meta.clients))
			} else {
				errs = append(errs, fmt.Sprintf("%s: required but not set", k))
			}
			continue
		}
		if val == "" {
			continue // optional + empty → skip further checks
		}
		switch meta.kind {
		case kindBool:
			if _, err := strconv.ParseBool(val); err != nil {
				errs = append(errs, fmt.Sprintf("%s: invalid boolean value %q", k, val))
			}
		case kindURL:
			parsed, err := url.ParseRequestURI(val)
			if err != nil || parsed.Host == "" {
				errs = append(errs, fmt.Sprintf("%s: not a valid absolute URL (must include scheme and host): %q", k, val))
			}
		case kindEnum:
			valid := false
			for _, ev := range meta.enumValues {
				if val == ev {
					valid = true
					break
				}
			}
			if !valid {
				errs = append(errs, fmt.Sprintf("%s: invalid value %q — must be one of %v", k, val, meta.enumValues))
			}
		}
	}
	return errs
}

// LoadFromCluster reads the aep-cli-config ConfigMap from the given namespace
// and loads each entry into viper via SetDefault, so CLI flags and AEP_* env
// vars still take precedence. Returns the number of keys loaded and nil if the
// ConfigMap does not yet exist (i.e. before `aectl init` has run).
func LoadFromCluster(ctx context.Context, client kubernetes.Interface, namespace string) (int, error) {
	cm, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, ConfigMapName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read %s ConfigMap: %w", ConfigMapName, err)
	}
	for k, v := range cm.Data {
		viper.SetDefault(k, v)
	}
	return len(cm.Data), nil
}

// LoadThunderSecretFromCluster reads the Thunder admin client secret from the
// ESO-synced ThunderAdminCredsSecret and sets it via viper.SetDefault so
// that AEP_THUNDER_ADMIN_CLIENT_SECRET env and the interactive prompt still
// take precedence. The secret is never stored in the ConfigMap.
// Returns nil if the Secret does not yet exist (first install).
func LoadThunderSecretFromCluster(ctx context.Context, client kubernetes.Interface, namespace string) error {
	sec, err := client.CoreV1().Secrets(namespace).Get(ctx, ThunderAdminCredsSecret, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", ThunderAdminCredsSecret, err)
	}
	v, ok := sec.Data[ThunderAdminCredsSecretKey]
	if !ok || len(v) == 0 {
		return fmt.Errorf("%s is missing non-empty key %q — ESO sync may be incomplete", ThunderAdminCredsSecret, ThunderAdminCredsSecretKey)
	}
	viper.SetDefault("thunder.admin_client_secret", string(v))
	return nil
}
