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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	// The OpenChoreo secret's keys, as aep-api's identity_targets.go reads them.
	credentialKeyClientID     = "clientId"
	credentialKeyClientSecret = "clientSecret"
)

// BindingName is the Secret/ConfigMap name writeBindingRecord uses for a given
// (org, env) pair — exported so other consumers of the binding record (e.g.
// the thunder-app-operator addon's WaitForSecrets precondition) name the same
// Secret without duplicating the format string.
func BindingName(org, env string) string { return fmt.Sprintf("thunder-binding-%s-%s", org, env) }

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
// OpenChoreo secret holding the credential, and annotations on the
// Environment.
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

	// Before the annotation, so it never names an unstored secret.
	if err := writeOpenChoreoBinding(ctx, cfg, inst); err != nil {
		return err
	}

	args := append([]string{"annotate", "environment", cfg.Env, "-n", cfg.Org, "--overwrite"}, bindingAnnotations(cfg, inst)...)
	if _, err := c.applyKubectl(ctx, args...); err != nil {
		return fmt.Errorf("annotate Environment %s/%s: %w", cfg.Org, cfg.Env, err)
	}
	return nil
}

func bindingAnnotations(cfg Config, inst *ThunderInstance) []string {
	name := BindingName(cfg.Org, cfg.Env)
	return []string{
		"aep.wso2.com/thunder-binding=" + name,
		"aep.wso2.com/thunder-issuer=" + inst.PublicURL,
		"aep.wso2.com/thunder-admin-url=" + inst.AdminURL,
		"aep.wso2.com/thunder-system-resource-identifier=" + inst.SystemResourceIdentifier,
		"aep.wso2.com/thunder-secret-ref=" + name,
	}
}

func bindingCredentialData(inst *ThunderInstance) map[string]string {
	return map[string]string{
		credentialKeyClientID:     "aep-system-client",
		credentialKeyClientSecret: inst.SystemClientSecret,
	}
}

func writeOpenChoreoBinding(ctx context.Context, cfg Config, inst *ThunderInstance) error {
	if cfg.BindingSecrets == nil {
		return fmt.Errorf("write OpenChoreo binding secret: envidp.Config.BindingSecrets is not set")
	}
	name := BindingName(cfg.Org, cfg.Env)
	if err := cfg.BindingSecrets.PutSecret(ctx, cfg.Org, name, bindingCredentialData(inst)); err != nil {
		return fmt.Errorf("write OpenChoreo binding secret %s/%s: %w", cfg.Org, name, err)
	}
	return nil
}
