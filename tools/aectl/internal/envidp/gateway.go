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
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"text/template"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/wso2/aep/aectl/internal/helm"
)

const (
	// gatewayEncryptionSecretName/Key match deployments/scripts/env.sh's
	// GATEWAY_ENCRYPTION_SECRET_NAME/KEY exactly: the name is set once on the
	// shared gateway OPERATOR (installed by aectl's own checkAPIPlatform
	// prerequisite) and applied to every gateway it deploys, so a different
	// name here would leave the controller unable to find its key.
	gatewayEncryptionSecretName = "api-platform-controller-aesgcm-key"
	gatewayEncryptionSecretKey  = "default-aesgcm256-v1.bin"
)

func gatewayNamespace(org, env string) string { return fmt.Sprintf("%s-%s", org, env) }
func gatewayRelease(org, env string) string   { return fmt.Sprintf("api-platform-%s-%s", org, env) }
func gatewayHostname(org, env string) string  { return fmt.Sprintf("%s-%s.gateway.localhost", env, org) }
func gatewayVhost(org, env string) string {
	return fmt.Sprintf("http://%s:19080", gatewayHostname(org, env))
}
func gatewayBackendJWTSecretName(release string) string { return release + "-backend-jwt" }
func gatewayTokenSecretName(release string) string      { return release + "-token" }

// assertion is the backend-JWT signing keypair's public half plus the
// metadata a downstream service verifies against — what gets published onto
// the Environment's annotations.
type assertion struct {
	issuer      string
	header      string
	certificate string
}

// installGateway installs (or binds to) this environment's API Platform
// gateway, wired to the given Thunder binding as its only ThunderKeyManager,
// and publishes its backend-JWT assertion certificate onto the Environment.
//
// bootstrap.enabled is always false here: this package never registers with
// Agent Manager (no amp-api call), matching
// setup-environment-gateway.sh's own behavior when amp-api does not answer —
// confirmed to still install and serve correctly, since Agent Manager
// registration is an addition to the release, not a precondition of it.
func installGateway(ctx context.Context, c clients, cfg Config, inst *ThunderInstance) error {
	namespace := gatewayNamespace(cfg.Org, cfg.Env)
	release := gatewayRelease(cfg.Org, cfg.Env)

	deployed, err := helm.ReleaseDeployed(ctx, cfg.Kubeconfig, release, namespace)
	if err != nil {
		return fmt.Errorf("check existing gateway release %s: %w", release, err)
	}

	if _, err := c.k8s.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create namespace %s: %w", namespace, err)
	}

	if _, err := ensureEncryptionKey(ctx, c, namespace); err != nil {
		return fmt.Errorf("ensure gateway encryption key: %w", err)
	}
	assert, signingKeyPEM, err := ensureBackendJWTKeypair(ctx, c, namespace, release, cfg.Org, cfg.Env)
	if err != nil {
		return fmt.Errorf("ensure backend-JWT signing keypair: %w", err)
	}
	if err := ensureControlPlaneTokenSecret(ctx, c, namespace, release); err != nil {
		return fmt.Errorf("ensure control-plane token secret: %w", err)
	}

	if !deployed {
		values, err := renderGatewayValues(cfg.Org, cfg.Env, namespace, inst, assert, signingKeyPEM)
		if err != nil {
			return fmt.Errorf("render gateway values: %w", err)
		}
		if err := helm.InstallChart(ctx, cfg.Kubeconfig, helm.ChartSpec{
			ReleaseName: release,
			Chart:       gatewayChart,
			Version:     gatewayChartVersion,
			Namespace:   namespace,
			Timeout:     "20m",
			ValuesYAML:  values,
		}); err != nil {
			return fmt.Errorf("install gateway chart %s: %w", release, err)
		}
	}

	if err := waitForGateway(ctx, c, namespace, release); err != nil {
		return fmt.Errorf("wait for gateway %s/%s: %w", namespace, release, err)
	}

	if _, err := c.applyKubectl(ctx, "annotate", "environment", cfg.Env, "-n", cfg.Org, "--overwrite",
		"aep.wso2.com/gateway-assertion-issuer="+assert.issuer,
		"aep.wso2.com/gateway-assertion-header="+assert.header,
		"aep.wso2.com/gateway-assertion-certificate="+assert.certificate,
	); err != nil {
		return fmt.Errorf("annotate Environment with gateway assertion: %w", err)
	}
	return nil
}

// ensureEncryptionKey generates a random 32-byte AES-256 key on first run and
// leaves it alone on every re-run: rotating it makes every already-encrypted
// gateway secret undecryptable.
func ensureEncryptionKey(ctx context.Context, c clients, namespace string) ([]byte, error) {
	existing, err := c.k8s.CoreV1().Secrets(namespace).Get(ctx, gatewayEncryptionSecretName, metav1.GetOptions{})
	if err == nil {
		if v := existing.Data[gatewayEncryptionSecretKey]; len(v) > 0 {
			return v, nil
		}
		return nil, fmt.Errorf("secret %s/%s exists but carries no %s key", namespace, gatewayEncryptionSecretName, gatewayEncryptionSecretKey)
	}
	if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("get secret %s/%s: %w", namespace, gatewayEncryptionSecretName, err)
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate encryption key: %w", err)
	}
	if _, err := c.k8s.CoreV1().Secrets(namespace).Apply(ctx,
		applySecret(namespace, gatewayEncryptionSecretName, map[string][]byte{gatewayEncryptionSecretKey: key}, nil),
		metav1.ApplyOptions{FieldManager: "aectl", Force: true},
	); err != nil {
		return nil, fmt.Errorf("store encryption key: %w", err)
	}
	return key, nil
}

// ensureBackendJWTKeypair generates a self-signed RSA-2048 keypair on first
// run (leaving it alone on re-runs, for the same reason as the encryption
// key), stores it as a typed kubernetes.io/tls Secret, and returns the
// assertion metadata a downstream service verifies against plus the
// private key's own PEM (needed by the caller to render the gateway's
// signingkey.inline values — see the ⚠️ note below on where that ends up).
// Ten-year validity: the certificate's expiry is not what retires this key —
// a deliberate rotation is (see the source comment in
// setup-environment-gateway.sh this is translated from).
func ensureBackendJWTKeypair(ctx context.Context, c clients, namespace, release, org, env string) (assertion, string, error) {
	secretName := gatewayBackendJWTSecretName(release)
	issuer := fmt.Sprintf("aep-gateway-%s-%s", org, env)
	header := "x-jwt-assertion"

	existing, err := c.k8s.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err == nil {
		cert := existing.Data[corev1.TLSCertKey]
		key := existing.Data[corev1.TLSPrivateKeyKey]
		if len(cert) == 0 || len(key) == 0 {
			return assertion{}, "", fmt.Errorf("secret %s/%s carries no %s/%s pair", namespace, secretName, corev1.TLSCertKey, corev1.TLSPrivateKeyKey)
		}
		return assertion{issuer: issuer, header: header, certificate: string(cert)}, string(key), nil
	}
	if !apierrors.IsNotFound(err) {
		return assertion{}, "", fmt.Errorf("get secret %s/%s: %w", namespace, secretName, err)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return assertion{}, "", fmt.Errorf("generate RSA key: %w", err)
	}
	certTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: issuer},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, certTemplate, certTemplate, &key.PublicKey, key)
	if err != nil {
		return assertion{}, "", fmt.Errorf("create self-signed certificate: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	if _, err := c.k8s.CoreV1().Secrets(namespace).Apply(ctx,
		applySecret(namespace, secretName, map[string][]byte{
			corev1.TLSPrivateKeyKey: keyPEM,
			corev1.TLSCertKey:       certPEM,
		}, nil).WithType(corev1.SecretTypeTLS),
		metav1.ApplyOptions{FieldManager: "aectl", Force: true},
	); err != nil {
		return assertion{}, "", fmt.Errorf("store backend-JWT keypair: %w", err)
	}

	// ⚠️ The private half reaches the gateway as signingkey.inline, rendered
	// into a ConfigMap in this namespace IN PLAINTEXT by the chart — the same
	// exposure setup-environment-gateway.sh documents and has not resolved
	// upstream. Anyone with namespace read here can read the signing key.
	return assertion{issuer: issuer, header: header, certificate: string(certPEM)}, string(keyPEM), nil
}

// ensureControlPlaneTokenSecret creates the APIGateway CR's tokenSecretRef
// Secret empty when it does not already exist. The CR always names this
// Secret and the gateway controller reads it as a non-optional env var, so
// with bootstrap.enabled false (no Agent Manager to register with and mint a
// real token) the pod sits in CreateContainerConfigError forever without it.
// Never overwritten: a later run with Agent Manager present must not clobber
// a real token the bootstrap Job already wrote here.
func ensureControlPlaneTokenSecret(ctx context.Context, c clients, namespace, release string) error {
	name := gatewayTokenSecretName(release)
	_, err := c.k8s.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get secret %s/%s: %w", namespace, name, err)
	}
	if _, err := c.k8s.CoreV1().Secrets(namespace).Apply(ctx,
		applySecret(namespace, name, map[string][]byte{"token": {}}, nil),
		metav1.ApplyOptions{FieldManager: "aectl", Force: true},
	); err != nil {
		return fmt.Errorf("create secret %s/%s: %w", namespace, name, err)
	}
	return nil
}

const gatewayValuesTemplate = `
agentManager:
  orgName: {{ .Org }}
gateway:
  environment: {{ .Env }}
  vhost: {{ .Vhost }}
apiGateway:
  namespace: {{ .Namespace }}
  config:
    policyConfigurations:
      jwtauth_v1:
        keymanagers:
          - name: agent-manager-service
            issuer: agent-manager-service
            jwks:
              remote:
                uri: http://amp-api.wso2-amp.svc.cluster.local:9000/auth/external/jwks.json
                skipTlsVerify: true
          - name: ThunderKeyManager
            issuer: {{ .ThunderIssuer }}
            jwks:
              remote:
                uri: {{ .ThunderJWKSURL }}
                skipTlsVerify: true
      backendjwt_v1:
        algorithm: SHA256withRSA
        issuer: {{ .AssertionIssuer }}
        tokenexpiry: 15m
        tokencaching: true
        signingkey:
          inline: |
{{ .SigningKeyIndented }}
bootstrap:
  enabled: false
`

type gatewayValuesData struct {
	Org, Env, Namespace                  string
	Vhost, ThunderIssuer, ThunderJWKSURL string
	AssertionIssuer                      string
	SigningKeyIndented                   string
}

// renderGatewayValues builds the values file setup-environment-gateway.sh
// itself builds — same shape, always the no-Agent-Manager branch
// (bootstrap.enabled: false, no identityProviders block).
func renderGatewayValues(org, env, namespace string, inst *ThunderInstance, assert assertion, signingKeyPEM string) (string, error) {
	tmpl, err := template.New("gateway-values").Parse(gatewayValuesTemplate)
	if err != nil {
		return "", err
	}
	indented := indentBlock(signingKeyPEM, "            ")
	var buf strings.Builder
	if err := tmpl.Execute(&buf, gatewayValuesData{
		Org:                org,
		Env:                env,
		Namespace:          namespace,
		Vhost:              gatewayVhost(org, env),
		ThunderIssuer:      inst.PublicURL,
		ThunderJWKSURL:     strings.TrimRight(inst.AdminURL, "/") + "/oauth2/jwks",
		AssertionIssuer:    assert.issuer,
		SigningKeyIndented: indented,
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func indentBlock(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

func waitForGateway(ctx context.Context, c clients, namespace, release string) error {
	if _, err := c.applyKubectl(ctx, "wait", "--for=condition=Programmed",
		"apigateway/"+release, "-n", namespace, "--timeout=300s"); err != nil {
		return fmt.Errorf("apigateway not programmed: %w", err)
	}
	runtimeSvc := release + "-gw-gateway-gateway-runtime"
	if _, err := c.applyKubectl(ctx, "wait", "--for=condition=Available",
		"deployment/"+runtimeSvc, "-n", namespace, "--timeout=300s"); err != nil {
		return fmt.Errorf("gateway runtime not available: %w", err)
	}
	return nil
}
