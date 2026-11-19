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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

// fakeKV is a KV v2 store under /v1/secret/data/, with check-and-set: a PUT
// carrying options.cas=0 is refused when the key exists, as OpenBao does.
type fakeKV struct {
	mu   sync.Mutex
	data map[string]string
}

func newFakeKV(t *testing.T, seed map[string]string) (*fakeKV, *openBaoSession) {
	t.Helper()
	kv := &fakeKV{data: map[string]string{}}
	for k, v := range seed {
		kv.data[k] = v
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/v1/secret/data/")
		kv.mu.Lock()
		defer kv.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			v, ok := kv.data[path]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errors":[]}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": map[string]any{"value": v}}})
		case http.MethodPut, http.MethodPost:
			var body struct {
				Options map[string]any    `json:"options"`
				Data    map[string]string `json:"data"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if cas, ok := body.Options["cas"]; ok && cas.(float64) == 0 {
				if _, exists := kv.data[path]; exists {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"errors":["check-and-set parameter did not match the current version"]}`))
					return
				}
			}
			kv.data[path] = body.Data["value"]
			_, _ = w.Write([]byte(`{"data":{}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return kv, &openBaoSession{baseURL: srv.URL, token: "t", stop: func() {}}
}

func (kv *fakeKV) get(path string) (string, bool) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	v, ok := kv.data[path]
	return v, ok
}

// A re-run install (make dev-env) on an intact store must leave every
// generated secret as it is: the Postgres volume, Thunder's registered
// clients and the GitHub hooks on the relay channel all hold the old values.
func TestWritePlatformSecrets_KeepsExistingGeneratedSecrets(t *testing.T) {
	existing := map[string]string{
		"aep/postgres-password":          "pg-old",
		"aep/opensearch-password":        "os-old",
		webhookRelaySeedPath:             "relay-old",
		"aep/idp-clients/aep-api-client": "api-old",
	}
	kv, s := newFakeKV(t, existing)
	inputs := []platformInput{{"aep/thunder-admin/client-secret", "admin-secret"}}
	if _, err := writePlatformSecrets(context.Background(), s, inputs, nil); err != nil {
		t.Fatal(err)
	}
	for path, want := range existing {
		if got, _ := kv.get(path); got != want {
			t.Errorf("%s was rewritten", path)
		}
	}
	// Inputs are written as given.
	if got, _ := kv.get("aep/thunder-admin/client-secret"); got != "admin-secret" {
		t.Errorf("thunder admin secret not written")
	}
	// Absent generated secrets are seeded.
	if got, ok := kv.get("aep/idp-clients/bff-git-service"); !ok || got == "" {
		t.Errorf("missing generated secret not seeded")
	}
}

// After an in-memory OpenBao restart a re-run install puts back the value the
// cluster still holds, not a new one: nothing that holds the old value breaks.
func TestWritePlatformSecrets_RestoresFromClusterBeforeGenerating(t *testing.T) {
	kv, s := newFakeKV(t, nil)
	recover := func(path string) (string, string, error) {
		if path == "aep/postgres-password" {
			return "pg-synced", "wso2-aep/postgres-secrets", nil
		}
		return "", "", nil
	}
	seeded, err := writePlatformSecrets(context.Background(), s, nil, recover)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := kv.get("aep/postgres-password"); got != "pg-synced" {
		t.Fatal("postgres password not restored from the cluster Secret")
	}
	if _, ok := kv.get("aep/jev-api-key"); ok {
		t.Error("an imported key with no cluster copy must stay absent")
	}
	if len(seeded) != len(generatedPlatformSecrets()) {
		t.Fatalf("seeded %d of %d", len(seeded), len(generatedPlatformSecrets()))
	}
	for _, sd := range seeded {
		if sd.path == "aep/postgres-password" && sd.from != "wso2-aep/postgres-secrets" {
			t.Errorf("restored key reported as %q", sd.from)
		}
		if sd.path != "aep/postgres-password" && (sd.from != "" || sd.value == "") {
			t.Errorf("%s: want a generated value", sd.path)
		}
	}
}

func TestSeedGeneratedSecrets(t *testing.T) {
	gen := []generatedSecret{{path: "aep/x", generate: func() (string, error) { return "fresh", nil }}}
	t.Run("present: no write", func(t *testing.T) {
		seeded, err := seedGeneratedSecrets(gen, seedStore{
			exists: func(string) (bool, error) { return true, nil },
			put:    func(p, _ string) error { t.Errorf("put %s on a present key", p); return nil },
		})
		if err != nil || len(seeded) != 0 {
			t.Fatalf("seeded=%v err=%v", seeded, err)
		}
	})
	t.Run("raced: another writer's key is kept", func(t *testing.T) {
		seeded, err := seedGeneratedSecrets(gen, seedStore{
			exists: func(string) (bool, error) { return false, nil },
			put:    func(string, string) error { return errSecretExists },
		})
		if err != nil || len(seeded) != 0 {
			t.Fatalf("seeded=%v err=%v", seeded, err)
		}
	})
	t.Run("cluster read fails: error, no write", func(t *testing.T) {
		_, err := seedGeneratedSecrets(gen, seedStore{
			exists:  func(string) (bool, error) { return false, nil },
			put:     func(string, string) error { t.Error("put after a failed cluster read"); return nil },
			recover: func(string) (string, string, error) { return "", "", errors.New("forbidden") },
		})
		if err == nil || !strings.Contains(err.Error(), "aep/x") {
			t.Fatalf("want an error naming the path, got %v", err)
		}
	})
	t.Run("write fails: error without the value", func(t *testing.T) {
		_, err := seedGeneratedSecrets(gen, seedStore{
			exists: func(string) (bool, error) { return false, nil },
			put:    func(string, string) error { return errors.New("500") },
		})
		if err == nil || strings.Contains(err.Error(), "fresh") {
			t.Fatalf("want an error that does not carry the value, got %v", err)
		}
	})
}

// The relay seed is 64 hex chars (`platform secret import` trims values).
func TestWebhookRelaySeedSecret_Hex(t *testing.T) {
	v, err := webhookRelaySeedSecret.generate()
	if err != nil || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(v) {
		t.Fatalf("seed must be 64 hex chars, got %d chars (err %v)", len(v), err)
	}
}

func esObject(ns, name, target string, data ...[3]string) unstructured.Unstructured {
	var items []interface{}
	for _, d := range data {
		items = append(items, map[string]interface{}{
			"secretKey": d[0],
			"remoteRef": map[string]interface{}{"key": d[1], "property": d[2]},
		})
	}
	spec := map[string]interface{}{"data": items}
	if target != "" {
		spec["target"] = map[string]interface{}{"name": target}
	}
	return unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": externalSecretAPIVersion, "kind": "ExternalSecret",
		"metadata": map[string]interface{}{"name": name, "namespace": ns},
		"spec":     spec,
	}}
}

// fakeES serves List/Get/Patch of ExternalSecrets from memory.
type fakeES struct {
	items   []unstructured.Unstructured
	patched []string
	// onPatch lets a test play ESO: it updates the object's status.
	onPatch func(obj *unstructured.Unstructured)
}

func (f *fakeES) List(_ context.Context, _, _, ns string) ([]unstructured.Unstructured, error) {
	var out []unstructured.Unstructured
	for _, it := range f.items {
		if ns == "" || it.GetNamespace() == ns {
			out = append(out, it)
		}
	}
	return out, nil
}

func (f *fakeES) Get(_ context.Context, _, _, ns, name string) (*unstructured.Unstructured, error) {
	for i := range f.items {
		if f.items[i].GetNamespace() == ns && f.items[i].GetName() == name {
			return &f.items[i], nil
		}
	}
	return nil, nil
}

func (f *fakeES) Patch(_ context.Context, _, _, ns, name string, _ types.PatchType, _ []byte) error {
	f.patched = append(f.patched, name)
	for i := range f.items {
		if f.items[i].GetNamespace() == ns && f.items[i].GetName() == name && f.onPatch != nil {
			f.onPatch(&f.items[i])
		}
	}
	return nil
}

func secret(ns, name, key, value string) *corev1.Secret {
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}, Data: map[string][]byte{key: []byte(value)}}
}

// The platform namespace's Secret wins; a blank or missing one falls through;
// a path no ExternalSecret maps recovers nothing.
func TestClusterSecretRecovery(t *testing.T) {
	es := &fakeES{items: []unstructured.Unstructured{
		esObject("obs", "observer-secret", "", [3]string{"K", "aep/idp-clients/oc-observer-reader", "value"}),
		esObject("wso2-aep", "aep-idp-client-secrets", "", [3]string{"OC_OBSERVER_READER_SECRET", "aep/idp-clients/oc-observer-reader", "value"}),
		esObject("wso2-aep", "postgres-secrets", "", [3]string{"POSTGRES_PASSWORD", "aep/postgres-password", "value"}),
		esObject("wso2-aep", "other", "", [3]string{"X", "aep/multi", "field"}),
	}}
	c := fake.NewSimpleClientset(
		secret("obs", "observer-secret", "K", "obs-copy"),
		secret("wso2-aep", "aep-idp-client-secrets", "OC_OBSERVER_READER_SECRET", "platform-copy"),
		secret("wso2-aep", "postgres-secrets", "POSTGRES_PASSWORD", ""),
		secret("wso2-aep", "other", "X", "never"),
	)
	recover, err := clusterSecretRecovery(context.Background(), es, c, "wso2-aep")
	if err != nil {
		t.Fatal(err)
	}
	if v, from, _ := recover("aep/idp-clients/oc-observer-reader"); v != "platform-copy" || from != "wso2-aep/aep-idp-client-secrets" {
		t.Errorf("want the platform namespace's copy, got from %q", from)
	}
	if v, _, _ := recover("aep/postgres-password"); v != "" {
		t.Error("a blank Secret must recover nothing")
	}
	if v, _, _ := recover("aep/multi"); v != "" {
		t.Error("a non-value property must not be read")
	}
	if v, _, _ := recover("aep/unmapped"); v != "" {
		t.Error("an unmapped path must recover nothing")
	}
}

func setStatus(obj *unstructured.Unstructured, refreshed time.Time, ready string) {
	_ = unstructured.SetNestedField(obj.Object, refreshed.UTC().Format(time.RFC3339), "status", "refreshTime")
	_ = unstructured.SetNestedSlice(obj.Object, []interface{}{map[string]interface{}{"type": "Ready", "status": ready}}, "status", "conditions")
}

// Only ExternalSecrets that read a platform path are nudged; one that syncs
// after the nudge is in sync, one that does not is reported by its Secret.
func TestSyncExternalSecretsNow(t *testing.T) {
	stale := time.Now().Add(-time.Hour)
	es := &fakeES{items: []unstructured.Unstructured{
		esObject("wso2-aep", "postgres-secrets", "", [3]string{"POSTGRES_PASSWORD", "aep/postgres-password", "value"}),
		esObject("wso2-aep", "aep-idp-client-secrets", "", [3]string{"AEP_API_CLIENT_SECRET", "aep/idp-clients/aep-api-client", "value"}),
		esObject("wso2-aep", "aep-jev", "", [3]string{"K", "aep/jev-api-key", "value"}),
	}}
	for i := range es.items {
		setStatus(&es.items[i], stale, "True")
	}
	es.onPatch = func(obj *unstructured.Unstructured) {
		if obj.GetName() == "postgres-secrets" {
			setStatus(obj, time.Now(), "True") // ESO re-read it
		}
	}
	unsynced, err := syncExternalSecretsNow(context.Background(), es, "wso2-aep",
		[]string{"aep/postgres-password", "aep/idp-clients/aep-api-client"}, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(unsynced) != 1 || unsynced[thunderSecretsName] == "" {
		t.Fatalf("unsynced = %v", unsynced)
	}
	for _, n := range es.patched {
		if n == "aep-jev" {
			t.Error("an ExternalSecret reading no platform path was nudged")
		}
	}
	skip := clientsReadingUnsynced(unsynced)
	if skip["aep-api-client"] == "" || skip["aep-console-client"] != "" {
		t.Fatalf("skip = %v", skip)
	}
}

// An imported key (the Jev key) is restored from the cluster after an OpenBao
// restart, so it need not be imported again.
func TestWritePlatformSecrets_RestoresImportedKey(t *testing.T) {
	kv, s := newFakeKV(t, nil)
	recover := func(path string) (string, string, error) {
		if path == "aep/jev-api-key" {
			return "jev-synced", "wso2-aep/aep-jev", nil
		}
		return "", "", nil
	}
	if _, err := writePlatformSecrets(context.Background(), s, nil, recover); err != nil {
		t.Fatal(err)
	}
	if got, _ := kv.get("aep/jev-api-key"); got != "jev-synced" {
		t.Fatal("Jev key not restored")
	}
}

// A role that already takes the Secret's password is left alone: an ALTER is
// harmless, but restarting its clients is not free.
func TestApplyPostgresRolePassword(t *testing.T) {
	var altered, restarted int
	st := postgresRoleSteps{
		authenticates: func(context.Context, string) bool { return true },
		alter:         func(context.Context, string) error { altered++; return nil },
		restartClients: func(context.Context) ([]string, error) {
			restarted++
			return []string{"aep-api", "temporal-frontend"}, nil
		},
	}
	if changed, err := applyPostgresRolePassword(context.Background(), "pw", st); err != nil || changed || altered+restarted != 0 {
		t.Fatalf("in sync: changed=%t err=%v altered=%d restarted=%d", changed, err, altered, restarted)
	}
	// A changed role password strands every running client on the old one:
	// temporal-frontend then fails each DB call while it stays Ready.
	st.authenticates = func(context.Context, string) bool { return false }
	if changed, err := applyPostgresRolePassword(context.Background(), "pw", st); err != nil || !changed || altered != 1 || restarted != 1 {
		t.Fatalf("drifted: changed=%t err=%v altered=%d restarted=%d", changed, err, altered, restarted)
	}
	st.alter = func(context.Context, string) error { return errors.New("boom") }
	if _, err := applyPostgresRolePassword(context.Background(), "pw", st); err == nil || restarted != 1 {
		t.Fatalf("failed ALTER must not restart: err=%v restarted=%d", err, restarted)
	}
}

func TestDeploymentsReadingSecret(t *testing.T) {
	dep := func(name string, c corev1.Container) *appsv1.Deployment {
		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: name},
			Spec:       appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{c}}}},
		}
	}
	ref := func(secret string) corev1.Container {
		return corev1.Container{Env: []corev1.EnvVar{{Name: "P", ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret}, Key: "k"}}}}}
	}
	from := corev1.Container{EnvFrom: []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "postgres-secrets"}}}}}
	c := fake.NewSimpleClientset(
		dep("aep-api", ref("postgres-secrets")),
		dep("temporal-frontend", from),
		dep("aep-console", ref("other")),
	)
	got, err := deploymentsReadingSecret(context.Background(), c, "ns", "postgres-secrets")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "aep-api,temporal-frontend" {
		t.Fatalf("got %v", got)
	}
}
