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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/wso2/aep/aectl/internal/openbao"
)

// Binding-record label/key names. These MUST match
// tools/thunder-app-operator's own binding.go (labelBindingKind/Org/Env,
// keyIssuer/keyAdminURL/keySystemResource/keySecretName/keySecretNamespace,
// keyClientID/keyClientSecret) and setup-environment-thunder.sh's own
// naming — the binding record is a contract read by three independent
// consumers (the operator, the gateway install, aep-api), none of which this
// package can change unilaterally.
const (
	labelBindingKind = "aep.wso2.com/kind"
	labelBindingOrg  = "aep.wso2.com/org"
	labelBindingEnv  = "aep.wso2.com/env"
	bindingKind      = "thunder-binding"

	keyIssuer          = "issuer"
	keyAdminURL        = "adminURL"
	keySystemResource  = "systemResourceIdentifier"
	keySecretName      = "secretName"
	keySecretNamespace = "secretNamespace"

	keyClientID     = "client-id"
	keyClientSecret = "client-secret"

	// openBaoKeyClientID/openBaoKeyClientSecret are the OpenBao projection's
	// own key names — camelCase, matching setup-environment-thunder.sh's own
	// write and aep-api's identity_targets.go credentialKeyClientID/
	// credentialKeyClientSecret. A different casing than keyClientID/
	// keyClientSecret above is deliberate: those name the K8s Secret mirrored
	// into thunder-app-operator-system, a separate consumer with its own
	// (kebab-case) contract.
	openBaoKeyClientID     = "clientId"
	openBaoKeyClientSecret = "clientSecret"
)

// BindingName is the Secret/ConfigMap name writeBindingRecord uses for a given
// (org, env) pair — exported so other consumers of the binding record (e.g.
// the thunder-app-operator addon's WaitForSecrets precondition) name the same
// Secret without duplicating the format string.
func BindingName(org, env string) string { return fmt.Sprintf("thunder-binding-%s-%s", org, env) }

// openBaoKVMount is the OpenBao KV-v2 mount every platform secret is written
// under (see cmd.provisionOpenBao's own "/v1/secret/data/..." calls) —
// matching setup-environment-thunder.sh's OPENBAO_MOUNT.
const openBaoKVMount = "secret"

// openBaoBindingPath is where writeOpenBaoBinding stores its projection of the
// binding record, relative to openBaoKVMount.
func openBaoBindingPath(org, env string) string { return fmt.Sprintf("aep/thunder/%s/%s", org, env) }

// openBaoBindingSecretPath is openBaoBindingPath WITH its mount prefix — the
// form the Environment's aep.wso2.com/thunder-secret-path annotation carries.
// aep-api's own OpenBao reader is mount-relative, so it validates this prefix
// against its configured mount and strips it before reading.
func openBaoBindingSecretPath(org, env string) string {
	return openBaoKVMount + "/" + openBaoBindingPath(org, env)
}

func bindingLabels(org, env string) map[string]string {
	return map[string]string{
		labelBindingKind: bindingKind,
		labelBindingOrg:  org,
		labelBindingEnv:  env,
	}
}

// bindingConfigMapData is the non-secret half of the binding record — see
// writeBindingRecord's doc comment for where each projection is read from.
// Extracted as a pure function (rather than inlined in writeBindingRecord)
// so its exact field set is unit-testable without executing any apply call.
func bindingConfigMapData(name string, inst *ThunderInstance) map[string]string {
	return map[string]string{
		keyIssuer:          inst.PublicURL,
		keyAdminURL:        inst.AdminURL,
		keySystemResource:  inst.SystemResourceIdentifier,
		keySecretName:      name,
		keySecretNamespace: operatorNamespace,
	}
}

// bindingSecretData is the credential half of the binding record, mirrored
// into thunder-app-operator-system. Same "pure builder, unit-testable
// without an apply call" rationale as bindingConfigMapData.
func bindingSecretData(inst *ThunderInstance) map[string][]byte {
	return map[string][]byte{
		keyClientID:     []byte("aep-system-client"),
		keyClientSecret: []byte(inst.SystemClientSecret),
	}
}

// writeBindingRecord writes the 4 projections of the binding record described
// in deployments/design/two-tier-thunder.md: a ConfigMap in T2's own
// namespace (the non-secret half, read by consumers that already know which
// T2 they're looking at), a Secret mirrored into thunder-app-operator-system
// (the only namespace that operator's Secret informer/RBAC can see), an
// OpenBao copy (aep-api runs outside the cluster and has no Kubernetes
// access), and annotations on the Environment (aep-api's other read path,
// for the same reason).
func writeBindingRecord(ctx context.Context, c clients, cfg Config, inst *ThunderInstance) error {
	name := BindingName(cfg.Org, cfg.Env)
	labels := bindingLabels(cfg.Org, cfg.Env)

	if _, err := c.k8s.CoreV1().ConfigMaps(inst.Namespace).Apply(ctx,
		applyConfigMap(inst.Namespace, name, bindingConfigMapData(name, inst), labels), metav1.ApplyOptions{FieldManager: "aectl", Force: true},
	); err != nil {
		return fmt.Errorf("apply binding ConfigMap %s/%s: %w", inst.Namespace, name, err)
	}

	// thunder-app-operator is an optional addon, installed (if at all) AFTER
	// this step in runAEPInit — so its namespace commonly does not exist yet
	// on a fresh install. Create it ourselves rather than fail: a plain
	// namespace with a Secret sitting in it is harmless whether or not the
	// operator is ever installed, and this is the same forward-reference
	// tolerance the rest of this codebase relies on elsewhere (e.g. RBAC
	// bound to a not-yet-existing ServiceAccount).
	if _, err := c.k8s.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: operatorNamespace},
	}, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create namespace %s: %w", operatorNamespace, err)
	}

	if _, err := c.k8s.CoreV1().Secrets(operatorNamespace).Apply(ctx,
		applySecret(operatorNamespace, name, bindingSecretData(inst), labels), metav1.ApplyOptions{FieldManager: "aectl", Force: true},
	); err != nil {
		return fmt.Errorf("mirror binding Secret to %s/%s: %w", operatorNamespace, name, err)
	}

	if err := writeOpenBaoBinding(ctx, cfg, inst); err != nil {
		return fmt.Errorf("write OpenBao binding copy: %w", err)
	}

	if _, err := c.applyKubectl(ctx, "annotate", "environment", cfg.Env, "-n", cfg.Org, "--overwrite",
		"aep.wso2.com/thunder-binding="+name,
		"aep.wso2.com/thunder-issuer="+inst.PublicURL,
		"aep.wso2.com/thunder-admin-url="+inst.AdminURL,
		"aep.wso2.com/thunder-system-resource-identifier="+inst.SystemResourceIdentifier,
		"aep.wso2.com/thunder-secret-path="+openBaoBindingSecretPath(cfg.Org, cfg.Env),
	); err != nil {
		return fmt.Errorf("annotate Environment %s/%s: %w", cfg.Org, cfg.Env, err)
	}
	return nil
}

// writeOpenBaoBinding writes the OpenBao projection at
// secret/aep/thunder/<org>/<env>, following exactly the port-forward +
// Kubernetes-auth + KV-v2-write pattern cmd.provisionOpenBao already uses for
// every other platform secret (internal/openbao's PortForward/
// WaitForReachable/GetSAToken/KubernetesLogin/Must) — same mechanism, new
// path, so a second, independent OpenBao client was not needed.
func writeOpenBaoBinding(ctx context.Context, cfg Config, inst *ThunderInstance) error {
	pfCmd, err := openbao.PortForward(ctx, cfg.OpenBaoNamespace, cfg.OpenBaoRelease, cfg.Kubeconfig)
	if err != nil {
		return err
	}
	defer func() { _ = pfCmd.Process.Kill() }()

	baseURL := "http://localhost:" + openbao.LocalPort
	if err := openbao.WaitForReachable(ctx, baseURL, 30*time.Second); err != nil {
		return fmt.Errorf("OpenBao not reachable via port-forward: %w", err)
	}

	saToken, err := openbao.GetSAToken(ctx, cfg.OpenBaoNamespace, cfg.OpenBaoServiceAccount, cfg.Kubeconfig)
	if err != nil {
		return err
	}
	token, err := openbao.KubernetesLogin(ctx, baseURL, cfg.OpenBaoWriteRole, saToken)
	if err != nil {
		return err
	}

	path := openBaoBindingPath(cfg.Org, cfg.Env)
	_, err = openbao.Must(ctx, "PUT", baseURL, token, "/v1/secret/data/"+path, map[string]interface{}{
		"data": map[string]interface{}{
			keyIssuer:              inst.PublicURL,
			keyAdminURL:            inst.AdminURL,
			keySystemResource:      inst.SystemResourceIdentifier,
			openBaoKeyClientID:     "aep-system-client",
			openBaoKeyClientSecret: inst.SystemClientSecret,
		},
	})
	return err
}
