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

package envidp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/wso2/aep/aectl/internal/helm"
	"github.com/wso2/aep/aectl/internal/thunder"
)

// ThunderInstance describes an installed, reachable T2 Thunder — everything
// downstream steps (the binding record, the gateway) need to know about it.
type ThunderInstance struct {
	Release, Namespace       string
	AdminURL                 string // in-cluster
	PublicURL                string
	SystemResourceIdentifier string // "<PublicURL>/mcp"
	SystemClientSecret       string
}

// systemClientSecretName is the Secret this package's own system client
// secret is stored/reused from, matching setup-environment-thunder.sh's
// "<release>-aep-system-client" naming.
func systemClientSecretName(release string) string { return release + "-aep-system-client" }

// installThunder installs (or, if already deployed, binds to) this
// environment's T2 Thunder and returns everything the binding record and
// gateway need.
//
// CREATE/BIND mirrors setup-environment-thunder.sh's own publisher
// contract — never `helm upgrade` a release this process did not create. In
// BIND mode, the mint IS the probe (exactly what every consumer of this
// instance will do), so it is attempted first and cheaply; only when it
// fails — a release Agent Manager created, or one left behind by an earlier,
// interrupted run — is AEP's bootstrap bundle (re-)published into the
// existing instance via repairAepSystemClient, mirroring the shell script's
// own ensure_aep_system_client.
func installThunder(ctx context.Context, c clients, cfg Config) (*ThunderInstance, error) {
	release := releaseName(cfg.Org, cfg.Env)
	namespace := release

	deployed, err := helm.ReleaseDeployed(ctx, cfg.Kubeconfig, release, namespace)
	if err != nil {
		return nil, fmt.Errorf("check existing Thunder release %s: %w", release, err)
	}

	secret, err := resolveSystemClientSecret(ctx, c, namespace, release)
	if err != nil {
		return nil, err
	}

	inst := &ThunderInstance{
		Release:            release,
		Namespace:          namespace,
		AdminURL:           adminURL(release, namespace),
		PublicURL:          publicURL(cfg.Env),
		SystemClientSecret: secret,
	}
	inst.SystemResourceIdentifier = thunder.SystemResourceIdentifier(inst.PublicURL)

	if !deployed {
		if err := createThunder(ctx, c, cfg, inst); err != nil {
			return nil, err
		}
		if err := verifyThunderReachable(ctx, c, cfg, inst); err != nil {
			return nil, fmt.Errorf("verify Thunder %s/%s: %w", namespace, release, err)
		}
		return inst, nil
	}

	if probeErr := verifyThunderReachable(ctx, c, cfg, inst); probeErr != nil {
		if repairErr := repairAepSystemClient(ctx, c, cfg, inst); repairErr != nil {
			return nil, fmt.Errorf("aep-system-client cannot mint on existing release %s (%v), and repair failed: %w", release, probeErr, repairErr)
		}
		if err := verifyThunderReachable(ctx, c, cfg, inst); err != nil {
			return nil, fmt.Errorf("verify Thunder %s/%s after repair: %w", namespace, release, err)
		}
		// Repair can succeed with a secret resolveSystemClientSecret just
		// minted (it only reuses one from an existing Secret; it never
		// creates one) — persist it now so it survives this process exiting,
		// the same contract createThunder uses. Without this, the credential
		// would live only in Thunder's own aep-system-client application and
		// in memory, and the next run would mint yet another one instead of
		// reusing what is already live.
		if err := persistSystemClientSecret(ctx, c, inst); err != nil {
			return nil, fmt.Errorf("persist repaired system client secret %s/%s: %w", namespace, release, err)
		}
	}
	return inst, nil
}

// resolveSystemClientSecret reuses the secret already stored in
// <release>-aep-system-client if that Secret exists (never rotate a working
// credential), otherwise mints a new one. Checked before CREATE runs so a
// re-run after a partial failure does not orphan the secret a live instance
// was actually bootstrapped with.
func resolveSystemClientSecret(ctx context.Context, c clients, namespace, release string) (string, error) {
	name := systemClientSecretName(release)
	existing, err := c.k8s.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		if v := existing.Data["client-secret"]; len(v) > 0 {
			return string(v), nil
		}
		return "", fmt.Errorf("secret %s/%s exists but carries no client-secret key", namespace, name)
	}
	if !apierrors.IsNotFound(err) {
		return "", fmt.Errorf("get secret %s/%s: %w", namespace, name, err)
	}

	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate system client secret: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func createThunder(ctx context.Context, c clients, cfg Config, inst *ThunderInstance) error {
	if _, err := c.k8s.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: inst.Namespace},
	}, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create namespace %s: %w", inst.Namespace, err)
	}

	docs := renderBootstrapDocuments(cfg.Org, cfg.Env, inst.SystemClientSecret, inst.SystemResourceIdentifier)
	bootstrapCM := inst.Release + "-bootstrap"
	data := make(map[string]string, len(docs))
	names := make([]string, 0, len(docs))
	for _, d := range docs {
		data[d.name] = d.content
		names = append(names, d.name)
	}
	sort.Strings(names) // ThunderID imports in lexical order; documented dependency order.
	if _, err := c.k8s.CoreV1().ConfigMaps(inst.Namespace).Apply(ctx,
		applyConfigMap(inst.Namespace, bootstrapCM, data, nil), metav1.ApplyOptions{FieldManager: "aectl", Force: true},
	); err != nil {
		return fmt.Errorf("apply bootstrap ConfigMap %s/%s: %w", inst.Namespace, bootstrapCM, err)
	}

	values, err := thunderChartSpec(inst, bootstrapCM, names, platformTrustFrom(cfg.PlatformThunderPublicURL))
	if err != nil {
		return fmt.Errorf("build Thunder chart spec: %w", err)
	}
	if err := helm.InstallChart(ctx, cfg.Kubeconfig, values); err != nil {
		return fmt.Errorf("install Thunder chart %s: %w", inst.Release, err)
	}

	return persistSystemClientSecret(ctx, c, inst)
}

// persistSystemClientSecret stores inst.SystemClientSecret into the canonical
// <release>-aep-system-client Secret. Both CREATE (createThunder, above) and a
// successful BIND-path repair (installThunder) must persist through this same
// call — resolveSystemClientSecret only ever reuses a value already sitting in
// this Secret, so any path that mints a new one and never writes it back here
// leaves the cluster's record of the credential out of sync with what is
// actually live in Thunder.
func persistSystemClientSecret(ctx context.Context, c clients, inst *ThunderInstance) error {
	if _, err := c.k8s.CoreV1().Secrets(inst.Namespace).Apply(ctx,
		applySecret(inst.Namespace, systemClientSecretName(inst.Release), map[string][]byte{
			"client-id":     []byte("aep-system-client"),
			"client-secret": []byte(inst.SystemClientSecret),
		}, nil), metav1.ApplyOptions{FieldManager: "aectl", Force: true},
	); err != nil {
		return fmt.Errorf("store system client secret %s/%s: %w", inst.Namespace, systemClientSecretName(inst.Release), err)
	}
	return nil
}

// verifyThunderReachable is the end-to-end gate: port-forward to the fresh
// (or existing) instance and authenticate as aep-system-client. thunder.New
// already performs an authenticated admin API call as part of resolving the
// default OU (GET /organization-units/tree/default) — a successful return is
// equivalent proof to the shell scripts' own gate ("a scope=system token
// minted WITH the resource indicator must reach an admin endpoint with 200").
func verifyThunderReachable(ctx context.Context, c clients, cfg Config, inst *ThunderInstance) error {
	pf, err := thunder.PortForwardService(ctx, inst.Namespace, inst.Release+"-service", cfg.Kubeconfig)
	if err != nil {
		return fmt.Errorf("port-forward to Thunder: %w", err)
	}
	defer pf.Stop()

	localURL := "http://localhost:" + pf.Port
	if err := thunder.WaitForReachable(ctx, localURL, 2*time.Minute, pf); err != nil {
		return fmt.Errorf("thunder not reachable via port-forward: %w", err)
	}
	if _, err := thunder.New(ctx, localURL, "aep-system-client", inst.SystemClientSecret, inst.SystemResourceIdentifier); err != nil {
		return fmt.Errorf("authenticate as aep-system-client: %w", err)
	}
	return nil
}

// platformTrustAudience is the audience T2 requires on a token minted by the
// platform IdP. Agent Manager's amp-api is the only caller that presents one
// today and mints it with this value (its own provisioning script's
// PLATFORM_THUNDER_TOKEN_AUDIENCE default), so the two must agree or every
// call it makes into this environment's IdP is rejected as wrong-audience.
const platformTrustAudience = "urn:wso2:amp"

// platformTrustJWKSPort is the HTTPS port the platform IdP's JWKS endpoint is
// reached on. ThunderID refuses to start at all on a plain-http trusted-issuer
// JWKS URL —
//
//	Failed to load configurations: trusted_issuer.jwks_url must use https
//	(got http://...); http is only allowed for localhost
//
// — and the certificate is issued for the public hostname, so the JWKS URL is
// HTTPS on this port while the issuer stays the plain-http public URL that T1
// actually stamps into `iss`. The two being different schemes is deliberate,
// not an oversight to tidy up.
const platformTrustJWKSPort = 8443

// platformTrust is what T2 needs in order to accept a token the platform IdP
// (T1) issued: the issuer string to match, where to fetch T1's signing keys,
// and the audience to require. Without it T2 trusts only itself, and a caller
// holding a perfectly valid T1 token is rejected.
type platformTrust struct {
	Issuer   string
	JWKSURL  string
	Audience string
}

// platformTrustFrom derives T2's trust of T1 from the platform IdP's public
// URL. Returns the zero value when that URL is unset, which leaves the
// trustedIssuer block off the install entirely rather than writing a half
// one: a trustedIssuer naming an issuer that signs nothing is worse than no
// trustedIssuer, because it reads as configured.
func platformTrustFrom(platformPublicURL string) platformTrust {
	host := hostnameOf(platformPublicURL)
	if platformPublicURL == "" || host == "" {
		return platformTrust{}
	}
	return platformTrust{
		Issuer:   platformPublicURL,
		JWKSURL:  fmt.Sprintf("https://%s:%d/oauth2/jwks", host, platformTrustJWKSPort),
		Audience: platformTrustAudience,
	}
}

// configured reports whether there is a trust to write.
func (t platformTrust) configured() bool { return t.Issuer != "" && t.JWKSURL != "" }

// thunderChartSpec builds the Thunder chart's install values — same shape as
// setup-environment-thunder.sh's own CREATE path: sqlite for every one of
// ThunderID 1.0.0's 4 logical DBs (single-writer, matching the official k3d
// guide's own single-replica setup), persistence for SQLite's on-disk file,
// the same httproute/publicUrl/jwt.issuer wiring setup-env-for-aectl.sh uses
// for the platform IdP, applied here for this environment's own hostname
// (see publicURL). Extracted as a pure function (fileNames already sorted by
// the caller) so the exact args are unit-testable without invoking helm.
func thunderChartSpec(inst *ThunderInstance, bootstrapCM string, fileNames []string, trust platformTrust) (helm.ChartSpec, error) {
	filesJSON, err := json.Marshal(fileNames)
	if err != nil {
		return helm.ChartSpec{}, fmt.Errorf("marshal bootstrap file list: %w", err)
	}
	setStrings := []string{
		fmt.Sprintf("fullnameOverride=%s", inst.Release),
		fmt.Sprintf("httproute.hostnames[0]=%s", hostnameOf(inst.PublicURL)),
		fmt.Sprintf("configuration.server.publicUrl=%s", inst.PublicURL),
		fmt.Sprintf("configuration.jwt.issuer=%s", inst.PublicURL),
	}
	if trust.configured() {
		setStrings = append(setStrings,
			fmt.Sprintf("configuration.server.security.trustedIssuer.issuer=%s", trust.Issuer),
			fmt.Sprintf("configuration.server.security.trustedIssuer.jwksUrl=%s", trust.JWKSURL),
			fmt.Sprintf("configuration.server.security.trustedIssuer.audience=%s", trust.Audience),
		)
	}
	return helm.ChartSpec{
		ReleaseName: inst.Release,
		Chart:       thunderChart,
		Version:     thunderChartVersion,
		Namespace:   inst.Namespace,
		Timeout:     "10m",
		Sets: []string{
			"deployment.replicaCount=1",
			"hpa.enabled=false",
			"ingress.enabled=false",
			"httproute.enabled=true",
			"httproute.parentRefs[0].name=gateway-default",
			"httproute.parentRefs[0].namespace=openchoreo-control-plane",
			"configuration.server.httpOnly=true",
			"configuration.database.config.type=sqlite",
			"configuration.database.runtime_transient.type=sqlite",
			"configuration.database.entity.type=sqlite",
			"configuration.database.runtime_persistent.type=sqlite",
			"persistence.enabled=true",
			"setup.enabled=true",
			fmt.Sprintf("bootstrap.configMap.name=%s", bootstrapCM),
		},
		SetStrings: setStrings,
		SetJSON: []string{
			fmt.Sprintf("bootstrap.configMap.files=%s", filesJSON),
		},
	}, nil
}
