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

package controller

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/wso2/aep/thunder-app-operator/api/v1alpha1"
	"github.com/wso2/aep/thunder-app-operator/internal/thunder"
)

// fakeAdmin is a recording stand-in for thunder.AdminClient.
type fakeAdmin struct {
	ensureCalls []thunder.DesiredApp
	deleteCalls []string
	originCalls [][]string
	clientID    string
	ensureErr   error
	deleteErr   error
	originErr   error
}

func (f *fakeAdmin) EnsureApplication(_ context.Context, app thunder.DesiredApp) (string, error) {
	f.ensureCalls = append(f.ensureCalls, app)
	if f.ensureErr != nil {
		return "", f.ensureErr
	}
	cid := f.clientID
	if cid == "" {
		cid = app.Name
	}
	return cid, nil
}

func (f *fakeAdmin) DeleteApplication(_ context.Context, name string) error {
	f.deleteCalls = append(f.deleteCalls, name)
	return f.deleteErr
}

func (f *fakeAdmin) SetBrowserOrigins(_ context.Context, origins []string) error {
	f.originCalls = append(f.originCalls, append([]string(nil), origins...))
	return f.originErr
}

// lastOrigins is the origin set of the most recent SetBrowserOrigins call,
// sorted so a test asserts on the SET, not on CR iteration order.
func (f *fakeAdmin) lastOrigins(t *testing.T) []string {
	t.Helper()
	if len(f.originCalls) == 0 {
		t.Fatalf("SetBrowserOrigins was never called")
	}
	got := append([]string(nil), f.originCalls[len(f.originCalls)-1]...)
	sort.Strings(got)
	return got
}

// The (org, environment) every test CR is rendered into, and the binding record
// that says which Thunder serves it. Mirrors the real shapes: the ConfigMap
// lives in the target Thunder's namespace, the Secret in the operator's own.
const (
	testOrg      = "test-org"
	testEnv      = "test-env"
	testPodNS    = "thunder-app-operator-system"
	testIssuer   = "http://test-env-idp.amp.localhost:8080"
	testAdminURL = "http://thunder-test-org-test-env-service.thunder-test-org-test-env.svc.cluster.local:8090"
)

// bindingFor builds both halves of a binding record for (org, env).
func bindingFor(org, env, issuer, adminURL, clientSecret string) []client.Object {
	name := "thunder-binding-" + org + "-" + env
	labels := map[string]string{
		labelBindingKind: bindingKind,
		labelBindingOrg:  org,
		labelBindingEnv:  env,
	}
	return []client.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: "thunder-" + org + "-" + env, Name: name, Labels: labels},
			Data: map[string]string{
				keyIssuer:          issuer,
				keyAdminURL:        adminURL,
				keySystemResource:  issuer + "/mcp",
				keySecretName:      name,
				keySecretNamespace: testPodNS,
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: testPodNS, Name: name, Labels: labels},
			Data: map[string][]byte{
				keyClientID:     []byte("aep-system-client"),
				keyClientSecret: []byte(clientSecret),
			},
		},
	}
}

// adminFactory stands in for thunder.New: it records the Config each target was
// built with and hands out one fakeAdmin per admin URL.
type adminFactory struct {
	configs []thunder.Config
	byURL   map[string]*fakeAdmin
	fixed   *fakeAdmin
}

func (f *adminFactory) new(cfg thunder.Config) thunder.AdminClient {
	f.configs = append(f.configs, cfg)
	if f.fixed != nil {
		return f.fixed
	}
	if f.byURL == nil {
		f.byURL = map[string]*fakeAdmin{}
	}
	if a, ok := f.byURL[cfg.BaseURL]; ok {
		return a
	}
	a := &fakeAdmin{}
	f.byURL[cfg.BaseURL] = a
	return a
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add v1alpha1 to scheme: %v", err)
	}
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("add corev1 to scheme: %v", err)
	}
	return s
}

// newReconciler wires a reconciler whose every target resolves to admin, with
// the default (testOrg, testEnv) binding record already in the cluster.
func newReconciler(t *testing.T, admin *fakeAdmin, objs ...client.Object) (*Reconciler, client.Client) {
	t.Helper()
	r, cl, _ := newReconcilerWithFactory(t, &adminFactory{fixed: admin},
		append(bindingFor(testOrg, testEnv, testIssuer, testAdminURL, "binding-secret"), objs...)...)
	return r, cl
}

// newReconcilerWithFactory seeds exactly the objects given — no implicit
// binding — so a test can say what the cluster holds.
func newReconcilerWithFactory(t *testing.T, f *adminFactory, objs ...client.Object) (*Reconciler, client.Client, *adminFactory) {
	t.Helper()
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.ThunderApplication{}).
		WithObjects(objs...).
		Build()
	return &Reconciler{
		Client:           cl,
		Scheme:           scheme,
		PodNamespace:     testPodNS,
		NewThunderClient: f.new,
	}, cl, f
}

func reqFor(app *v1alpha1.ThunderApplication) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Namespace: app.Namespace, Name: app.Name}}
}

// newApp builds a CR labelled the way OpenChoreo's renderedrelease-controller
// labels one — those labels are what points it at a Thunder.
func newApp(ns, name string, spec v1alpha1.ThunderApplicationSpec) *v1alpha1.ThunderApplication {
	return newAppIn(ns, name, testOrg, testEnv, spec)
}

func newAppIn(ns, name, org, env string, spec v1alpha1.ThunderApplicationSpec) *v1alpha1.ThunderApplication {
	return &v1alpha1.ThunderApplication{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  ns,
			Name:       name,
			Generation: 1,
			Labels: map[string]string{
				labelCPNamespace: org,
				labelEnvironment: env,
			},
		},
		Spec: spec,
	}
}

// (a) Fresh CR: finalizer added, EnsureApplication called with derived name +
// split scopes/redirectURIs, ConfigMap published with owner ref, status ready.
func TestReconcile_FreshCR(t *testing.T) {
	app := newApp("test-ns", "my-app", v1alpha1.ThunderApplicationSpec{
		DisplayName:  "My App",
		Scopes:       "openid profile email group ou",
		RedirectURIs: "https://a.example.com,https://b.example.com",
	})
	admin := &fakeAdmin{clientID: "cid-123"}
	r, cl := newReconciler(t, admin, app)

	res, err := r.Reconcile(context.Background(), reqFor(app))
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if res.RequeueAfter != 0 {
		t.Errorf("unexpected RequeueAfter on success: %v", res.RequeueAfter)
	}

	if len(admin.ensureCalls) != 1 {
		t.Fatalf("EnsureApplication called %d times, want 1", len(admin.ensureCalls))
	}
	got := admin.ensureCalls[0]
	if got.Name != "aep-test-ns-my-app" {
		t.Errorf("DesiredApp.Name = %q, want aep-test-ns-my-app", got.Name)
	}
	if !reflect.DeepEqual(got.Scopes, []string{"openid", "profile", "email", "group", "ou"}) {
		t.Errorf("Scopes = %#v, want [openid profile email group ou]", got.Scopes)
	}
	if !reflect.DeepEqual(got.RedirectURIs, []string{"https://a.example.com", "https://b.example.com"}) {
		t.Errorf("RedirectURIs = %#v", got.RedirectURIs)
	}

	// ConfigMap published with client_id and a controller owner reference.
	var cm corev1.ConfigMap
	if err := cl.Get(context.Background(), types.NamespacedName{Namespace: "test-ns", Name: "my-app-oauth"}, &cm); err != nil {
		t.Fatalf("get oauth ConfigMap: %v", err)
	}
	if cm.Data["client_id"] != "cid-123" {
		t.Errorf("ConfigMap client_id = %q, want cid-123", cm.Data["client_id"])
	}
	if len(cm.OwnerReferences) != 1 || cm.OwnerReferences[0].Name != "my-app" ||
		cm.OwnerReferences[0].Controller == nil || !*cm.OwnerReferences[0].Controller {
		t.Errorf("ConfigMap owner references = %#v, want controller ref to my-app", cm.OwnerReferences)
	}

	// Status reflects readiness.
	var updated v1alpha1.ThunderApplication
	if err := cl.Get(context.Background(), reqFor(app).NamespacedName, &updated); err != nil {
		t.Fatalf("get CR: %v", err)
	}
	if !updated.Status.Ready {
		t.Errorf("Status.Ready = false, want true")
	}
	if updated.Status.ClientID != "cid-123" {
		t.Errorf("Status.ClientID = %q, want cid-123", updated.Status.ClientID)
	}
	if updated.Status.ObservedGeneration != updated.Generation {
		t.Errorf("Status.ObservedGeneration = %d, want %d", updated.Status.ObservedGeneration, updated.Generation)
	}
	if !containsFinalizer(updated.Finalizers, thunderFinalizer) {
		t.Errorf("finalizer %q not present: %#v", thunderFinalizer, updated.Finalizers)
	}
}

// Empty RedirectURIs -> empty slice (no stray empty elements).
func TestReconcile_EmptyRedirectURIs(t *testing.T) {
	app := newApp("ns1", "app1", v1alpha1.ThunderApplicationSpec{Scopes: "openid"})
	admin := &fakeAdmin{}
	r, _ := newReconciler(t, admin, app)

	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if len(admin.ensureCalls) != 1 {
		t.Fatalf("EnsureApplication called %d times, want 1", len(admin.ensureCalls))
	}
	if len(admin.ensureCalls[0].RedirectURIs) != 0 {
		t.Errorf("RedirectURIs = %#v, want empty", admin.ensureCalls[0].RedirectURIs)
	}
}

// (b) Spec change: EnsureApplication called again with the new redirect URIs.
func TestReconcile_SpecChange(t *testing.T) {
	app := newApp("ns", "app", v1alpha1.ThunderApplicationSpec{
		Scopes:       "openid",
		RedirectURIs: "https://old.example.com",
	})
	admin := &fakeAdmin{clientID: "cid"}
	r, cl := newReconciler(t, admin, app)

	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}

	var cur v1alpha1.ThunderApplication
	if err := cl.Get(context.Background(), reqFor(app).NamespacedName, &cur); err != nil {
		t.Fatalf("get CR: %v", err)
	}
	cur.Spec.RedirectURIs = "https://new.example.com"
	if err := cl.Update(context.Background(), &cur); err != nil {
		t.Fatalf("update CR spec: %v", err)
	}

	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}

	if len(admin.ensureCalls) != 2 {
		t.Fatalf("EnsureApplication called %d times, want 2", len(admin.ensureCalls))
	}
	if !reflect.DeepEqual(admin.ensureCalls[1].RedirectURIs, []string{"https://new.example.com"}) {
		t.Errorf("second call RedirectURIs = %#v", admin.ensureCalls[1].RedirectURIs)
	}
}

// (c) AdminClient error: ready=false, message set, RequeueAfter>0, no error.
func TestReconcile_AdminError(t *testing.T) {
	app := newApp("ns", "app", v1alpha1.ThunderApplicationSpec{Scopes: "openid"})
	admin := &fakeAdmin{ensureErr: errors.New("thunder is down")}
	r, cl := newReconciler(t, admin, app)

	res, err := r.Reconcile(context.Background(), reqFor(app))
	if err != nil {
		t.Fatalf("Reconcile should not return an error on Thunder failure: %v", err)
	}
	if res.RequeueAfter <= 0 {
		t.Errorf("RequeueAfter = %v, want > 0", res.RequeueAfter)
	}

	var updated v1alpha1.ThunderApplication
	if err := cl.Get(context.Background(), reqFor(app).NamespacedName, &updated); err != nil {
		t.Fatalf("get CR: %v", err)
	}
	if updated.Status.Ready {
		t.Errorf("Status.Ready = true, want false")
	}
	if updated.Status.Message == "" {
		t.Errorf("Status.Message is empty, want error detail")
	}
}

// (d) Deletion: DeleteApplication called, finalizer removed (CR is GC'd).
func TestReconcile_Deletion(t *testing.T) {
	now := metav1.Now()
	app := newApp("ns", "app", v1alpha1.ThunderApplicationSpec{Scopes: "openid"})
	app.DeletionTimestamp = &now
	app.Finalizers = []string{thunderFinalizer}
	admin := &fakeAdmin{}
	r, cl := newReconciler(t, admin, app)

	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if len(admin.deleteCalls) != 1 || admin.deleteCalls[0] != "aep-ns-app" {
		t.Errorf("DeleteApplication calls = %#v, want [aep-ns-app]", admin.deleteCalls)
	}

	// Finalizer removed -> fake client garbage-collects the CR.
	var updated v1alpha1.ThunderApplication
	err := cl.Get(context.Background(), reqFor(app).NamespacedName, &updated)
	if err == nil {
		if containsFinalizer(updated.Finalizers, thunderFinalizer) {
			t.Errorf("finalizer still present after delete: %#v", updated.Finalizers)
		}
	} else if !apierrors.IsNotFound(err) {
		t.Fatalf("unexpected error getting CR: %v", err)
	}
}

// Deletion with a Thunder error keeps the finalizer and requeues.
func TestReconcile_DeletionThunderError(t *testing.T) {
	now := metav1.Now()
	app := newApp("ns", "app", v1alpha1.ThunderApplicationSpec{Scopes: "openid"})
	app.DeletionTimestamp = &now
	app.Finalizers = []string{thunderFinalizer}
	admin := &fakeAdmin{deleteErr: errors.New("boom")}
	r, cl := newReconciler(t, admin, app)

	res, err := r.Reconcile(context.Background(), reqFor(app))
	if err != nil {
		t.Fatalf("Reconcile should not return an error: %v", err)
	}
	if res.RequeueAfter <= 0 {
		t.Errorf("RequeueAfter = %v, want > 0", res.RequeueAfter)
	}

	var updated v1alpha1.ThunderApplication
	if err := cl.Get(context.Background(), reqFor(app).NamespacedName, &updated); err != nil {
		t.Fatalf("get CR: %v", err)
	}
	if !containsFinalizer(updated.Finalizers, thunderFinalizer) {
		t.Errorf("finalizer removed despite Thunder error: %#v", updated.Finalizers)
	}
}

// Confidential client: spec.clientId is used as the Thunder name, secret is
// read from the referenced Kubernetes Secret.
func TestReconcile_ConfidentialClient(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "my-secrets"},
		Data:       map[string][]byte{"MY_SECRET": []byte("s3cr3t")},
	}
	app := newApp("ns", "svc-client", v1alpha1.ThunderApplicationSpec{
		DisplayName: "My Service",
		ClientType:  "confidential",
		ClientID:    "my-service-client",
		SecretRef:   &v1alpha1.SecretKeyRef{Name: "my-secrets", Key: "MY_SECRET"},
	})
	admin := &fakeAdmin{clientID: "my-service-client"}
	r, cl := newReconciler(t, admin, app, secret)

	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	if len(admin.ensureCalls) != 1 {
		t.Fatalf("EnsureApplication called %d times, want 1", len(admin.ensureCalls))
	}
	got := admin.ensureCalls[0]
	if got.Name != "my-service-client" {
		t.Errorf("DesiredApp.Name = %q, want my-service-client", got.Name)
	}
	if got.ClientType != "confidential" {
		t.Errorf("DesiredApp.ClientType = %q, want confidential", got.ClientType)
	}
	if got.ClientSecret != "s3cr3t" {
		t.Errorf("DesiredApp.ClientSecret = %q, want s3cr3t", got.ClientSecret)
	}

	// ConfigMap carries the explicit client_id.
	var cm corev1.ConfigMap
	if err := cl.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "svc-client-oauth"}, &cm); err != nil {
		t.Fatalf("get oauth ConfigMap: %v", err)
	}
	if cm.Data["client_id"] != "my-service-client" {
		t.Errorf("ConfigMap client_id = %q, want my-service-client", cm.Data["client_id"])
	}
}

// Confidential client with missing secretRef → error, CR marked not ready.
func TestReconcile_ConfidentialClient_MissingSecretRef(t *testing.T) {
	app := newApp("ns", "broken", v1alpha1.ThunderApplicationSpec{
		ClientType: "confidential",
		ClientID:   "broken-client",
		// SecretRef intentionally omitted
	})
	admin := &fakeAdmin{}
	r, cl := newReconciler(t, admin, app)

	res, err := r.Reconcile(context.Background(), reqFor(app))
	if err != nil {
		t.Fatalf("Reconcile should not return an error: %v", err)
	}
	if res.RequeueAfter <= 0 {
		t.Errorf("RequeueAfter = %v, want > 0 (should retry)", res.RequeueAfter)
	}
	if len(admin.ensureCalls) != 0 {
		t.Errorf("EnsureApplication called %d times, want 0", len(admin.ensureCalls))
	}

	var updated v1alpha1.ThunderApplication
	if err := cl.Get(context.Background(), reqFor(app).NamespacedName, &updated); err != nil {
		t.Fatalf("get CR: %v", err)
	}
	if updated.Status.Ready {
		t.Errorf("Status.Ready = true, want false")
	}
}

// Confidential client where the Secret exists in a different namespace (not the
// CR's namespace) → CR marked not ready, EnsureApplication not called. This
// proves the controller enforces same-namespace Secret access only.
func TestReconcile_ConfidentialClient_SecretWrongNamespace(t *testing.T) {
	// Secret is in "other-ns", CR is in "ns" — controller must not find it.
	secretInOtherNS := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "other-ns", Name: "my-secrets"},
		Data:       map[string][]byte{"MY_SECRET": []byte("s3cr3t")},
	}
	app := newApp("ns", "svc-client", v1alpha1.ThunderApplicationSpec{
		ClientType: "confidential",
		ClientID:   "my-service-client",
		SecretRef:  &v1alpha1.SecretKeyRef{Name: "my-secrets", Key: "MY_SECRET"},
	})
	admin := &fakeAdmin{clientID: "my-service-client"}
	r, cl := newReconciler(t, admin, app, secretInOtherNS)

	res, err := r.Reconcile(context.Background(), reqFor(app))
	if err != nil {
		t.Fatalf("Reconcile should not return an error: %v", err)
	}
	if res.RequeueAfter <= 0 {
		t.Errorf("RequeueAfter = %v, want > 0 (should retry)", res.RequeueAfter)
	}
	if len(admin.ensureCalls) != 0 {
		t.Errorf("EnsureApplication called %d times, want 0", len(admin.ensureCalls))
	}

	var updated v1alpha1.ThunderApplication
	if err := cl.Get(context.Background(), reqFor(app).NamespacedName, &updated); err != nil {
		t.Fatalf("get CR: %v", err)
	}
	if updated.Status.Ready {
		t.Errorf("Status.Ready = true, want false")
	}
}

// Unsupported clientType → CR marked not ready, EnsureApplication not called.
func TestReconcile_UnsupportedClientType(t *testing.T) {
	app := newApp("ns", "bad-type", v1alpha1.ThunderApplicationSpec{
		ClientType: "bearer", // not public or confidential
		ClientID:   "bad-type-client",
	})
	admin := &fakeAdmin{}
	r, cl := newReconciler(t, admin, app)

	res, err := r.Reconcile(context.Background(), reqFor(app))
	if err != nil {
		t.Fatalf("Reconcile should not return an error: %v", err)
	}
	if res.RequeueAfter <= 0 {
		t.Errorf("RequeueAfter = %v, want > 0 (should retry)", res.RequeueAfter)
	}
	if len(admin.ensureCalls) != 0 {
		t.Errorf("EnsureApplication called %d times, want 0", len(admin.ensureCalls))
	}

	var updated v1alpha1.ThunderApplication
	if err := cl.Get(context.Background(), reqFor(app).NamespacedName, &updated); err != nil {
		t.Fatalf("get CR: %v", err)
	}
	if updated.Status.Ready {
		t.Errorf("Status.Ready = true, want false")
	}
}

// spec.clientId override: Thunder name uses the explicit client ID, not derived.
func TestReconcile_ClientIDOverride(t *testing.T) {
	app := newApp("some-ns", "some-cr", v1alpha1.ThunderApplicationSpec{
		ClientID:     "my-explicit-id",
		Scopes:       "openid",
		RedirectURIs: "https://app.example.com",
	})
	admin := &fakeAdmin{}
	r, _ := newReconciler(t, admin, app)

	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if len(admin.ensureCalls) != 1 {
		t.Fatalf("EnsureApplication called %d times, want 1", len(admin.ensureCalls))
	}
	if admin.ensureCalls[0].Name != "my-explicit-id" {
		t.Errorf("DesiredApp.Name = %q, want my-explicit-id", admin.ensureCalls[0].Name)
	}
}

// Deletion uses spec.clientId when set.
func TestReconcile_Deletion_ClientIDOverride(t *testing.T) {
	now := metav1.Now()
	app := newApp("ns", "app", v1alpha1.ThunderApplicationSpec{ClientID: "explicit-id"})
	app.DeletionTimestamp = &now
	app.Finalizers = []string{thunderFinalizer}
	admin := &fakeAdmin{}
	r, _ := newReconciler(t, admin, app)

	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(admin.deleteCalls) != 1 || admin.deleteCalls[0] != "explicit-id" {
		t.Errorf("DeleteApplication calls = %#v, want [explicit-id]", admin.deleteCalls)
	}
}

// Secret rotation: after the referenced Secret's value changes the reconciler
// picks up the new client secret on the next reconcile (triggered by the
// Secret watch added to SetupWithManager).
func TestReconcile_ConfidentialClient_SecretRotation(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "my-secrets"},
		Data:       map[string][]byte{"MY_SECRET": []byte("original-secret")},
	}
	app := newApp("ns", "svc-client", v1alpha1.ThunderApplicationSpec{
		ClientType: "confidential",
		ClientID:   "my-service-client",
		SecretRef:  &v1alpha1.SecretKeyRef{Name: "my-secrets", Key: "MY_SECRET"},
	})
	admin := &fakeAdmin{clientID: "my-service-client"}
	r, cl := newReconciler(t, admin, app, secret)

	// First reconcile uses the original secret.
	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}
	if len(admin.ensureCalls) != 1 || admin.ensureCalls[0].ClientSecret != "original-secret" {
		t.Fatalf("first reconcile: ClientSecret = %q, want original-secret", admin.ensureCalls[0].ClientSecret)
	}

	// Rotate the secret value in the cluster.
	secret.Data["MY_SECRET"] = []byte("rotated-secret")
	if err := cl.Update(context.Background(), secret); err != nil {
		t.Fatalf("update Secret: %v", err)
	}

	// Second reconcile (as would be triggered by the Secret watch) picks up
	// the new value.
	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if len(admin.ensureCalls) != 2 {
		t.Fatalf("EnsureApplication called %d times, want 2", len(admin.ensureCalls))
	}
	if admin.ensureCalls[1].ClientSecret != "rotated-secret" {
		t.Errorf("after rotation: ClientSecret = %q, want rotated-secret", admin.ensureCalls[1].ClientSecret)
	}
}

// -- browser origins (CORS) -------------------------------------------------
//
// The app provisioned by a ThunderApplication is a BROWSER client of the IdP:
// it fetches the discovery document and exchanges its code with XHR, so the
// IdP has to name the app's origin or the browser drops both responses. The
// redirect URIs on the CR are the only place that origin appears anywhere in
// the platform.

// The origin is derived from the redirect URI (scheme + host + port, no path)
// and registered as part of a normal reconcile.
func TestReconcile_RegistersTheSPAOriginWithTheIdP(t *testing.T) {
	app := newApp("ns", "app", v1alpha1.ThunderApplicationSpec{
		RedirectURIs: "http://http-todo-web--org-env-abc.openchoreoapis.localhost:19080/callback",
	})
	admin := &fakeAdmin{}
	r, _ := newReconciler(t, admin, app)

	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	want := []string{"http://http-todo-web--org-env-abc.openchoreoapis.localhost:19080"}
	if got := admin.lastOrigins(t); !reflect.DeepEqual(got, want) {
		t.Errorf("origins = %#v, want %#v (scheme+host+port only — a path in the allow-list matches nothing)", got, want)
	}
}

// The allow-list is a PROJECTION of every app on the instance, not an append:
// one CR's reconcile must not drop a sibling's origin.
func TestReconcile_OriginSetIsTheUnionOfEveryAppOnTheInstance(t *testing.T) {
	one := newApp("ns", "one", v1alpha1.ThunderApplicationSpec{
		RedirectURIs: "https://one.example.com/callback",
	})
	two := newApp("other-ns", "two", v1alpha1.ThunderApplicationSpec{
		// Two URIs on one origin, plus one that shares `one`'s origin: the
		// allow-list is a set of origins, not a list of redirect URIs.
		RedirectURIs: "https://two.example.com/callback,https://two.example.com/silent,https://one.example.com/callback",
	})
	// Another environment's app on another Thunder never enters this set.
	elsewhere := newAppIn("far-ns", "far", testOrg, "other-env", v1alpha1.ThunderApplicationSpec{
		RedirectURIs: "https://far.example.com/callback",
	})
	admin := &fakeAdmin{}
	r, _ := newReconciler(t, admin, one, two, elsewhere)

	if _, err := r.Reconcile(context.Background(), reqFor(one)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	want := []string{"https://one.example.com", "https://two.example.com"}
	if got := admin.lastOrigins(t); !reflect.DeepEqual(got, want) {
		t.Errorf("origins = %#v, want %#v", got, want)
	}
}

// A redirect URI no browser could ever send does not widen the allow-list.
func TestReconcile_NonBrowserRedirectURIsAreNotOrigins(t *testing.T) {
	app := newApp("ns", "app", v1alpha1.ThunderApplicationSpec{
		RedirectURIs: "myapp://callback,/relative/callback,https://real.example.com/callback",
	})
	admin := &fakeAdmin{}
	r, _ := newReconciler(t, admin, app)

	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	want := []string{"https://real.example.com"}
	if got := admin.lastOrigins(t); !reflect.DeepEqual(got, want) {
		t.Errorf("origins = %#v, want %#v", got, want)
	}
}

// A CORS failure must not fail the CR: the OAuth app exists and everything
// downstream of the binding can proceed. It says so on status and retries.
func TestReconcile_OriginFailureKeepsTheAppReadyAndRetries(t *testing.T) {
	app := newApp("ns", "app", v1alpha1.ThunderApplicationSpec{
		RedirectURIs: "https://app.example.com/callback",
	})
	admin := &fakeAdmin{clientID: "cid-1", originErr: errors.New("thunder put cors returned 503")}
	r, cl := newReconciler(t, admin, app)

	res, err := r.Reconcile(context.Background(), reqFor(app))
	if err != nil {
		t.Fatalf("Reconcile should not return an error: %v", err)
	}
	if res.RequeueAfter <= 0 {
		t.Errorf("RequeueAfter = %v, want > 0 — the origin must be retried", res.RequeueAfter)
	}

	var updated v1alpha1.ThunderApplication
	if err := cl.Get(context.Background(), reqFor(app).NamespacedName, &updated); err != nil {
		t.Fatalf("get CR: %v", err)
	}
	if !updated.Status.Ready {
		t.Error("status.ready = false — a CORS failure must not block the app's consumers")
	}
	if updated.Status.ClientID != "cid-1" {
		t.Errorf("status.clientId = %q, want cid-1", updated.Status.ClientID)
	}
	if !strings.Contains(updated.Status.Message, "browser origins") {
		t.Errorf("status.message = %q, want it to name the unregistered origins", updated.Status.Message)
	}
}

// Deleting an app withdraws its origin and leaves its siblings' alone.
func TestReconcile_DeletionWithdrawsTheOrigin(t *testing.T) {
	now := metav1.Now()
	going := newApp("ns", "going", v1alpha1.ThunderApplicationSpec{
		RedirectURIs: "https://going.example.com/callback",
	})
	going.DeletionTimestamp = &now
	going.Finalizers = []string{thunderFinalizer}
	staying := newApp("ns", "staying", v1alpha1.ThunderApplicationSpec{
		RedirectURIs: "https://staying.example.com/callback",
	})
	admin := &fakeAdmin{}
	r, _ := newReconciler(t, admin, going, staying)

	if _, err := r.Reconcile(context.Background(), reqFor(going)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	want := []string{"https://staying.example.com"}
	if got := admin.lastOrigins(t); !reflect.DeepEqual(got, want) {
		t.Errorf("origins after delete = %#v, want %#v", got, want)
	}
}

// The last app on an instance leaves an EMPTY list, not the app's origin.
func TestReconcile_DeletingTheLastAppEmptiesTheAllowList(t *testing.T) {
	now := metav1.Now()
	app := newApp("ns", "only", v1alpha1.ThunderApplicationSpec{
		RedirectURIs: "https://only.example.com/callback",
	})
	app.DeletionTimestamp = &now
	app.Finalizers = []string{thunderFinalizer}
	admin := &fakeAdmin{}
	r, _ := newReconciler(t, admin, app)

	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got := admin.lastOrigins(t); len(got) != 0 {
		t.Errorf("origins after the last delete = %#v, want empty", got)
	}
}

// A CORS failure on the delete path must not wedge the CR: a stale entry in an
// allow-list is not worth an undeletable namespace.
func TestReconcile_DeletionReleasesTheCRWhenTheOriginCannotBeWithdrawn(t *testing.T) {
	now := metav1.Now()
	app := newApp("ns", "app", v1alpha1.ThunderApplicationSpec{
		RedirectURIs: "https://app.example.com/callback",
	})
	app.DeletionTimestamp = &now
	app.Finalizers = []string{thunderFinalizer}
	admin := &fakeAdmin{originErr: errors.New("boom")}
	r, cl := newReconciler(t, admin, app)

	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var updated v1alpha1.ThunderApplication
	err := cl.Get(context.Background(), reqFor(app).NamespacedName, &updated)
	if err == nil && containsFinalizer(updated.Finalizers, thunderFinalizer) {
		t.Error("finalizer still present — a CORS failure must not hold the CR")
	} else if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("unexpected error getting CR: %v", err)
	}
}

func containsFinalizer(finalizers []string, want string) bool {
	for _, f := range finalizers {
		if f == want {
			return true
		}
	}
	return false
}
