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

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// fakeExternalSecrets is the cluster's ExternalSecrets as the observer repoint
// sees them: one optional object, and a record of every apply.
type fakeExternalSecrets struct {
	obj     *unstructured.Unstructured
	applied []string
	gets    []string
}

func (f *fakeExternalSecrets) Get(_ context.Context, apiVersion, kind, ns, name string) (*unstructured.Unstructured, error) {
	f.gets = append(f.gets, apiVersion+" "+kind+" "+ns+"/"+name)
	return f.obj, nil
}

func (f *fakeExternalSecrets) ApplyYAML(_ context.Context, _, _ string, manifests string) error {
	f.applied = append(f.applied, manifests)
	return nil
}

// observerExternalSecret is observer-secret as an installer left it, reading
// the given vault key through the given store.
func observerExternalSecret(store, key string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "external-secrets.io/v1",
		"kind":       "ExternalSecret",
		"metadata":   map[string]any{"name": "observer-secret", "namespace": "openchoreo-observability-plane"},
		"spec": map[string]any{
			"refreshInterval": "1h",
			"secretStoreRef":  map[string]any{"kind": "ClusterSecretStore", "name": store},
			"target":          map[string]any{"name": "observer-secret"},
			"data": []any{map[string]any{
				"secretKey": "UID_RESOLVER_OAUTH_CLIENT_SECRET",
				"remoteRef": map[string]any{"key": key, "property": "value"},
			}},
		},
	}}
}

func TestPointObserverAtReaderSecret_PlaneAbsent_NoApply(t *testing.T) {
	es := &fakeExternalSecrets{}
	changed, err := pointObserverAtReaderSecret(context.Background(), es, defaultObsNamespace, defaultPlatformSecretStore)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if changed || len(es.applied) != 0 {
		t.Fatalf("changed=%v applied=%d, want no write when observer-secret is absent", changed, len(es.applied))
	}
	if len(es.gets) != 1 || es.gets[0] != "external-secrets.io/v1 ExternalSecret openchoreo-observability-plane/observer-secret" {
		t.Fatalf("gets = %v, want the observability plane's observer-secret", es.gets)
	}
}

func TestPointObserverAtReaderSecret_OldKey_PatchedToTheAEPKey(t *testing.T) {
	es := &fakeExternalSecrets{obj: observerExternalSecret("default", "observer-oauth-client-secret")}
	changed, err := pointObserverAtReaderSecret(context.Background(), es, defaultObsNamespace, defaultPlatformSecretStore)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !changed || len(es.applied) != 1 {
		t.Fatalf("changed=%v applied=%d, want one apply", changed, len(es.applied))
	}
	got := es.applied[0]
	// The SAME manifest `aectl sre install` applies: one source of truth.
	want, err := renderTemplate("sre-observer-secret", sreObserverClientSecretTmpl,
		sreParams{ObsNamespace: defaultObsNamespace, PlatformSecretStore: defaultPlatformSecretStore})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got != want {
		t.Errorf("applied manifest differs from sreObserverClientSecretTmpl\n--- got\n%s\n--- want\n%s", got, want)
	}
	for _, s := range []string{"key: aep/thunder-clients/oc-observer-reader", "name: aep-platform", "namespace: openchoreo-observability-plane"} {
		if !strings.Contains(got, s) {
			t.Errorf("applied manifest missing %q\n%s", s, got)
		}
	}
}

// A store mismatch is as wrong as a key mismatch: the AEP key lives only
// behind the platform's store.
func TestPointObserverAtReaderSecret_RightKeyWrongStore_Patched(t *testing.T) {
	es := &fakeExternalSecrets{obj: observerExternalSecret("default", observerReaderVaultKey)}
	changed, err := pointObserverAtReaderSecret(context.Background(), es, defaultObsNamespace, defaultPlatformSecretStore)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !changed || len(es.applied) != 1 {
		t.Fatalf("changed=%v applied=%d, want one apply", changed, len(es.applied))
	}
}

func TestPointObserverAtReaderSecret_AlreadyCorrect_NoWrite(t *testing.T) {
	es := &fakeExternalSecrets{obj: observerExternalSecret(defaultPlatformSecretStore, observerReaderVaultKey)}
	changed, err := pointObserverAtReaderSecret(context.Background(), es, defaultObsNamespace, defaultPlatformSecretStore)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if changed || len(es.applied) != 0 {
		t.Fatalf("changed=%v applied=%d, want no write when already pointed at the AEP key", changed, len(es.applied))
	}
}
