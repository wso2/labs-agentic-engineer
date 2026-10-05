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

package cmd

// observer_client_secret.go — keeping the observability plane's observer on
// the same Thunder client secret aectl registers for it.
//
// aectl owns openchoreo-observer-resource-reader-client: install and
// sync-clients set its Thunder secret to aep/thunder-clients/oc-observer-reader,
// replacing whatever the plane's installer registered. The observer reads its
// copy through the plane's observer-secret ExternalSecret, so whenever aectl
// makes that value authoritative it also points observer-secret at it — with
// the very manifest `aectl sre install` applies (sreObserverClientSecretTmpl).
// Otherwise a plane installed beside the platform, without `aectl sre`, keeps
// the installer's default and every observer scope lookup 401s.

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"

	k8s "github.com/wso2/aep/aectl/internal/kubernetes"
	"github.com/wso2/aep/aectl/internal/ui"
)

const (
	// defaultObsNamespace is where OpenChoreo's observability plane installs.
	defaultObsNamespace = "openchoreo-observability-plane"
	// defaultPlatformSecretStore is the ClusterSecretStore the platform chart
	// installs for the aep/* OpenBao paths.
	defaultPlatformSecretStore = "aep-platform"

	observerReaderClientID   = "openchoreo-observer-resource-reader-client"
	observerReaderVaultKey   = "aep/thunder-clients/oc-observer-reader"
	observerSecretName       = "observer-secret"
	observerSecretKey        = "UID_RESOLVER_OAUTH_CLIENT_SECRET"
	observerSecretRefreshCap = 2 * time.Minute
)

// externalSecretClient reads and applies ExternalSecrets; satisfied by
// *k8s.Applier.
type externalSecretClient interface {
	objectGetter
	yamlApplier
}

// pointObserverAtReaderSecret applies sreObserverClientSecretTmpl when the
// plane's observer-secret exists and does not already read
// observerReaderVaultKey through store. It reports whether it wrote. An absent
// ExternalSecret (no plane, or a plane that does not use one) is no write and
// no error. Only the reference is read and written, never the value.
func pointObserverAtReaderSecret(ctx context.Context, es externalSecretClient, obsNamespace, store string) (bool, error) {
	obj, err := es.Get(ctx, "external-secrets.io/v1", "ExternalSecret", obsNamespace, observerSecretName)
	if err != nil {
		return false, fmt.Errorf("read ExternalSecret %s/%s: %w", obsNamespace, observerSecretName, err)
	}
	if obj == nil || observerSecretReadsReader(obj, store) {
		return false, nil
	}
	p := sreParams{ObsNamespace: obsNamespace, PlatformSecretStore: store}
	if err := applyTemplate(ctx, es, "sre-observer-secret", obsNamespace, sreObserverClientSecretTmpl, p); err != nil {
		return false, fmt.Errorf("apply ExternalSecret %s/%s: %w", obsNamespace, observerSecretName, err)
	}
	return true, nil
}

// observerSecretReadsReader reports whether observer-secret already carries
// the observer's client secret from observerReaderVaultKey through store.
func observerSecretReadsReader(obj *unstructured.Unstructured, store string) bool {
	if name, _, _ := unstructured.NestedString(obj.Object, "spec", "secretStoreRef", "name"); name != store {
		return false
	}
	if kind, _, _ := unstructured.NestedString(obj.Object, "spec", "secretStoreRef", "kind"); kind != "ClusterSecretStore" {
		return false
	}
	data, _, _ := unstructured.NestedSlice(obj.Object, "spec", "data")
	for _, d := range data {
		m, ok := d.(map[string]any)
		if !ok || m["secretKey"] != observerSecretKey {
			continue
		}
		ref, _ := m["remoteRef"].(map[string]any)
		return ref["key"] == observerReaderVaultKey && ref["property"] == "value"
	}
	return false
}

// syncObserverClientSecret points the default observability plane's observer
// at the reader secret aectl just registered, then waits for ESO to rewrite
// the Secret and restarts the observer, which reads it only at start. Nothing
// here fails the caller: the Thunder clients are already registered, and a
// plane that could not be repointed is reported with what to run.
func syncObserverClientSecret(ctx context.Context, k8sClient *kubernetes.Clientset) {
	applier, err := k8s.NewApplier(kubeconfig)
	if err != nil {
		ui.Warn(fmt.Sprintf("Observer client secret not checked: %v", err))
		return
	}
	applied := time.Now()
	changed, err := pointObserverAtReaderSecret(ctx, applier, defaultObsNamespace, defaultPlatformSecretStore)
	switch {
	case err != nil:
		ui.Warn(fmt.Sprintf("Observer client secret not repointed: %v (re-run `aectl platform sync-clients`)", err))
		return
	case !changed:
		ui.Detail(fmt.Sprintf("Observer client secret: %s/%s absent or already on %s; nothing to do", defaultObsNamespace, observerSecretName, observerReaderVaultKey))
		return
	}
	if err := waitForExternalSecretRefresh(ctx, applier, defaultObsNamespace, observerSecretName, applied, observerSecretRefreshCap); err != nil {
		ui.Warn(fmt.Sprintf("Observer client secret repointed but not yet synced: %v (restart deploy/observer in %s once it is)", err, defaultObsNamespace))
		return
	}
	if err := rolloutRestart(ctx, k8sClient, defaultObsNamespace, "observer"); err != nil {
		ui.Warn(fmt.Sprintf("Observer client secret repointed; restart deploy/observer in %s to load it: %v", defaultObsNamespace, err))
		return
	}
	ui.Success(fmt.Sprintf("Observer pointed at %s (restarted)", observerReaderVaultKey))
}
