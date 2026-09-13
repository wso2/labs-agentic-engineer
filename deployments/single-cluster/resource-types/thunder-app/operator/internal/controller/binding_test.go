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
	"sort"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/wso2/aep/thunder-app-operator/api/v1alpha1"
)

// The binding resolves: the app is registered on THAT instance, and where it
// lives is published on status and in the -oauth ConfigMap.
func TestReconcile_PublishesResolvedTarget(t *testing.T) {
	app := newApp("dp-ns", "web", v1alpha1.ThunderApplicationSpec{Scopes: "openid"})
	f := &adminFactory{}
	r, cl, _ := newReconcilerWithFactory(t, f,
		append(bindingFor(testOrg, testEnv, testIssuer, testAdminURL, "binding-secret"), app)...)

	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if len(f.configs) != 1 {
		t.Fatalf("built %d clients, want 1", len(f.configs))
	}
	cfg := f.configs[0]
	if cfg.BaseURL != testAdminURL {
		t.Errorf("client BaseURL = %q, want %q", cfg.BaseURL, testAdminURL)
	}
	if cfg.ClientID != "aep-system-client" || cfg.ClientSecret != "binding-secret" {
		t.Errorf("client credentials = %q/%q, want the binding Secret's", cfg.ClientID, cfg.ClientSecret)
	}
	if cfg.SystemResourceIdentifier != testIssuer+"/mcp" {
		t.Errorf("SystemResourceIdentifier = %q, want %q", cfg.SystemResourceIdentifier, testIssuer+"/mcp")
	}

	var updated v1alpha1.ThunderApplication
	if err := cl.Get(context.Background(), reqFor(app).NamespacedName, &updated); err != nil {
		t.Fatalf("get CR: %v", err)
	}
	if updated.Status.Issuer != testIssuer {
		t.Errorf("Status.Issuer = %q, want %q", updated.Status.Issuer, testIssuer)
	}
	if updated.Status.JWKSURL != testIssuer+"/oauth2/jwks" {
		t.Errorf("Status.JWKSURL = %q, want %q", updated.Status.JWKSURL, testIssuer+"/oauth2/jwks")
	}
	if updated.Status.AdminURL != testAdminURL {
		t.Errorf("Status.AdminURL = %q, want %q", updated.Status.AdminURL, testAdminURL)
	}

	var cm corev1.ConfigMap
	if err := cl.Get(context.Background(), types.NamespacedName{Namespace: "dp-ns", Name: "web-oauth"}, &cm); err != nil {
		t.Fatalf("get oauth ConfigMap: %v", err)
	}
	if cm.Data["issuer"] != testIssuer || cm.Data["jwks_url"] != testIssuer+"/oauth2/jwks" {
		t.Errorf("ConfigMap issuer/jwks_url = %q/%q", cm.Data["issuer"], cm.Data["jwks_url"])
	}
	if cm.Data["client_id"] != "aep-dp-ns-web" {
		t.Errorf("ConfigMap client_id = %q, want aep-dp-ns-web", cm.Data["client_id"])
	}
}

// No binding for the CR's (org, env): Ready=false with a message that names
// what is missing, a requeue, and nothing attempted on any Thunder.
func TestReconcile_MissingBinding(t *testing.T) {
	app := newAppIn("dp-ns", "web", testOrg, "nope", v1alpha1.ThunderApplicationSpec{Scopes: "openid"})
	f := &adminFactory{}
	r, cl, _ := newReconcilerWithFactory(t, f,
		append(bindingFor(testOrg, testEnv, testIssuer, testAdminURL, "binding-secret"), app)...)

	res, err := r.Reconcile(context.Background(), reqFor(app))
	if err != nil {
		t.Fatalf("Reconcile should not return an error: %v", err)
	}
	if res.RequeueAfter <= 0 {
		t.Errorf("RequeueAfter = %v, want > 0", res.RequeueAfter)
	}
	if len(f.configs) != 0 {
		t.Errorf("built %d clients for an unresolvable target, want 0", len(f.configs))
	}

	var updated v1alpha1.ThunderApplication
	if err := cl.Get(context.Background(), reqFor(app).NamespacedName, &updated); err != nil {
		t.Fatalf("get CR: %v", err)
	}
	if updated.Status.Ready {
		t.Errorf("Status.Ready = true, want false")
	}
	for _, want := range []string{"no thunder binding", "org=" + testOrg, "env=nope", labelBindingKind + "=" + bindingKind} {
		if !strings.Contains(updated.Status.Message, want) {
			t.Errorf("Status.Message = %q, want it to contain %q", updated.Status.Message, want)
		}
	}
}

// A CR with no OpenChoreo (org, environment) labels cannot be pointed at any
// Thunder — say so instead of guessing one.
func TestReconcile_UnlabelledCR(t *testing.T) {
	app := newAppIn("dp-ns", "web", "", "", v1alpha1.ThunderApplicationSpec{Scopes: "openid"})
	f := &adminFactory{}
	r, cl, _ := newReconcilerWithFactory(t, f, app)

	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	var updated v1alpha1.ThunderApplication
	if err := cl.Get(context.Background(), reqFor(app).NamespacedName, &updated); err != nil {
		t.Fatalf("get CR: %v", err)
	}
	if !strings.Contains(updated.Status.Message, labelEnvironment) {
		t.Errorf("Status.Message = %q, want it to name the missing labels", updated.Status.Message)
	}
}

// The binding's ConfigMap is there but its Secret was never mirrored: the CR
// says which Secret is missing.
func TestReconcile_BindingSecretMissing(t *testing.T) {
	app := newApp("dp-ns", "web", v1alpha1.ThunderApplicationSpec{Scopes: "openid"})
	halves := bindingFor(testOrg, testEnv, testIssuer, testAdminURL, "binding-secret")
	f := &adminFactory{}
	// Seed the ConfigMap half only.
	r, cl, _ := newReconcilerWithFactory(t, f, halves[0], app)

	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	var updated v1alpha1.ThunderApplication
	if err := cl.Get(context.Background(), reqFor(app).NamespacedName, &updated); err != nil {
		t.Fatalf("get CR: %v", err)
	}
	if updated.Status.Ready {
		t.Errorf("Status.Ready = true, want false")
	}
	want := testPodNS + "/thunder-binding-" + testOrg + "-" + testEnv
	if !strings.Contains(updated.Status.Message, want) {
		t.Errorf("Status.Message = %q, want it to name %q", updated.Status.Message, want)
	}
}

// The binding mirrors its Secret somewhere this operator cannot read: say that,
// rather than reporting a Secret that "does not exist".
func TestReconcile_BindingSecretWrongNamespace(t *testing.T) {
	app := newApp("dp-ns", "web", v1alpha1.ThunderApplicationSpec{Scopes: "openid"})
	halves := bindingFor(testOrg, testEnv, testIssuer, testAdminURL, "binding-secret")
	cm, ok := halves[0].(*corev1.ConfigMap)
	if !ok {
		t.Fatalf("bindingFor[0] is %T, want *corev1.ConfigMap", halves[0])
	}
	cm.Data[keySecretNamespace] = "somewhere-else"
	f := &adminFactory{}
	r, cl, _ := newReconcilerWithFactory(t, f, cm, halves[1], app)

	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	var updated v1alpha1.ThunderApplication
	if err := cl.Get(context.Background(), reqFor(app).NamespacedName, &updated); err != nil {
		t.Fatalf("get CR: %v", err)
	}
	if !strings.Contains(updated.Status.Message, "somewhere-else") {
		t.Errorf("Status.Message = %q, want it to name the namespace it cannot read", updated.Status.Message)
	}
}

// Two CRs in two environments reach two different Thunders; a repeat pass
// reuses the cached client; a changed binding rebuilds it.
func TestClientCache_PerTargetAndInvalidation(t *testing.T) {
	const otherEnv = "other-env"
	otherIssuer := "http://other-env-idp.amp.localhost:8080"
	otherAdminURL := "http://thunder-test-org-other-env-service.thunder-test-org-other-env.svc.cluster.local:8090"

	appA := newApp("dp-a", "web", v1alpha1.ThunderApplicationSpec{Scopes: "openid"})
	appB := newAppIn("dp-b", "web", testOrg, otherEnv, v1alpha1.ThunderApplicationSpec{Scopes: "openid"})

	objs := bindingFor(testOrg, testEnv, testIssuer, testAdminURL, "secret-a")
	objs = append(objs, bindingFor(testOrg, otherEnv, otherIssuer, otherAdminURL, "secret-b")...)
	objs = append(objs, appA, appB)

	f := &adminFactory{}
	r, cl, _ := newReconcilerWithFactory(t, f, objs...)

	for _, app := range []*v1alpha1.ThunderApplication{appA, appB} {
		if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
			t.Fatalf("Reconcile %s: %v", app.Namespace, err)
		}
	}
	if len(f.configs) != 2 {
		t.Fatalf("built %d clients for two environments, want 2", len(f.configs))
	}
	urls := []string{f.configs[0].BaseURL, f.configs[1].BaseURL}
	sort.Strings(urls)
	if urls[0] != otherAdminURL || urls[1] != testAdminURL {
		t.Errorf("client base URLs = %v, want one per environment", urls)
	}
	if f.byURL[testAdminURL] == f.byURL[otherAdminURL] {
		t.Errorf("both environments got the same client")
	}
	// Each app went to its own Thunder.
	if got := f.byURL[testAdminURL].ensureCalls; len(got) != 1 || got[0].Name != "aep-dp-a-web" {
		t.Errorf("%s got %#v", testAdminURL, got)
	}
	if got := f.byURL[otherAdminURL].ensureCalls; len(got) != 1 || got[0].Name != "aep-dp-b-web" {
		t.Errorf("%s got %#v", otherAdminURL, got)
	}

	// A second pass over an unchanged binding reuses the cached client.
	if _, err := r.Reconcile(context.Background(), reqFor(appA)); err != nil {
		t.Fatalf("repeat Reconcile: %v", err)
	}
	if len(f.configs) != 2 {
		t.Errorf("built %d clients after a repeat pass, want 2 (cache hit)", len(f.configs))
	}

	// Rotating the binding credential rebuilds that environment's client only.
	var sec corev1.Secret
	name := types.NamespacedName{Namespace: testPodNS, Name: "thunder-binding-" + testOrg + "-" + testEnv}
	if err := cl.Get(context.Background(), name, &sec); err != nil {
		t.Fatalf("get binding Secret: %v", err)
	}
	sec.Data[keyClientSecret] = []byte("rotated")
	if err := cl.Update(context.Background(), &sec); err != nil {
		t.Fatalf("rotate binding Secret: %v", err)
	}
	if _, err := r.Reconcile(context.Background(), reqFor(appA)); err != nil {
		t.Fatalf("Reconcile after rotation: %v", err)
	}
	if len(f.configs) != 3 {
		t.Fatalf("built %d clients after rotation, want 3", len(f.configs))
	}
	if last := f.configs[2]; last.BaseURL != testAdminURL || last.ClientSecret != "rotated" {
		t.Errorf("rebuilt client = %q/%q, want %q/rotated", last.BaseURL, last.ClientSecret, testAdminURL)
	}
}

// Deleting a CR whose environment lost its binding: nothing can be reached, so
// the CR is released with the orphan logged rather than wedged forever.
func TestReconcileDelete_BindingGone(t *testing.T) {
	now := metav1.Now()
	app := newApp("dp-ns", "web", v1alpha1.ThunderApplicationSpec{Scopes: "openid"})
	app.DeletionTimestamp = &now
	app.Finalizers = []string{thunderFinalizer}
	app.Status = v1alpha1.ThunderApplicationStatus{
		Ready: true, ClientID: "aep-dp-ns-web", Issuer: testIssuer, AdminURL: testAdminURL,
	}
	f := &adminFactory{}
	r, cl, _ := newReconcilerWithFactory(t, f, app)

	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(f.configs) != 0 {
		t.Errorf("built %d clients with no binding, want 0", len(f.configs))
	}
	assertGone(t, cl, app)
}

// Deleting a CR whose binding ConfigMap is intact but whose Secret is missing:
// repairable, so the finalizer is kept and the delete retried.
func TestReconcileDelete_BindingSecretMissingKeepsFinalizer(t *testing.T) {
	now := metav1.Now()
	app := newApp("dp-ns", "web", v1alpha1.ThunderApplicationSpec{Scopes: "openid"})
	app.DeletionTimestamp = &now
	app.Finalizers = []string{thunderFinalizer}
	app.Status = v1alpha1.ThunderApplicationStatus{Ready: true, ClientID: "aep-dp-ns-web", AdminURL: testAdminURL}
	halves := bindingFor(testOrg, testEnv, testIssuer, testAdminURL, "binding-secret")
	f := &adminFactory{}
	r, cl, _ := newReconcilerWithFactory(t, f, halves[0], app)

	res, err := r.Reconcile(context.Background(), reqFor(app))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.RequeueAfter <= 0 {
		t.Errorf("RequeueAfter = %v, want > 0", res.RequeueAfter)
	}
	var updated v1alpha1.ThunderApplication
	if err := cl.Get(context.Background(), reqFor(app).NamespacedName, &updated); err != nil {
		t.Fatalf("get CR: %v", err)
	}
	if !containsFinalizer(updated.Finalizers, thunderFinalizer) {
		t.Errorf("finalizer dropped on a repairable binding: %#v", updated.Finalizers)
	}
}

// The environment's binding now names a different instance than the one this
// application was registered on: deleting there would remove nothing, so the
// operator does not try, and says the old application is left behind.
func TestReconcileDelete_BindingRepointed(t *testing.T) {
	now := metav1.Now()
	app := newApp("dp-ns", "web", v1alpha1.ThunderApplicationSpec{Scopes: "openid"})
	app.DeletionTimestamp = &now
	app.Finalizers = []string{thunderFinalizer}
	app.Status = v1alpha1.ThunderApplicationStatus{
		Ready: true, ClientID: "aep-dp-ns-web",
		Issuer: "http://old-idp.amp.localhost:8080", AdminURL: "http://old-service.old.svc.cluster.local:8090",
	}
	f := &adminFactory{}
	r, cl, _ := newReconcilerWithFactory(t, f,
		append(bindingFor(testOrg, testEnv, testIssuer, testAdminURL, "binding-secret"), app)...)

	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(f.configs) != 0 {
		t.Errorf("built %d clients for a re-pointed binding, want 0", len(f.configs))
	}
	assertGone(t, cl, app)
}

// A binding change re-queues exactly the CRs of that (org, environment).
func TestBindingToThunderApps(t *testing.T) {
	mine := newApp("dp-a", "web", v1alpha1.ThunderApplicationSpec{})
	other := newAppIn("dp-b", "web", testOrg, "other-env", v1alpha1.ThunderApplicationSpec{})
	halves := bindingFor(testOrg, testEnv, testIssuer, testAdminURL, "s")
	r, _, _ := newReconcilerWithFactory(t, &adminFactory{}, halves[0], halves[1], mine, other)

	for _, obj := range []client.Object{halves[0], halves[1]} {
		reqs := r.bindingToThunderApps(context.Background(), obj)
		if len(reqs) != 1 || reqs[0].Namespace != "dp-a" || reqs[0].Name != "web" {
			t.Errorf("%T mapped to %#v, want just dp-a/web", obj, reqs)
		}
	}

	// A ConfigMap that is not a binding record maps to nothing — that is what
	// keeps the operator's own -oauth ConfigMaps out of the queue.
	plain := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "dp-a", Name: "web-oauth"}}
	if reqs := r.bindingToThunderApps(context.Background(), plain); len(reqs) != 0 {
		t.Errorf("a non-binding ConfigMap mapped to %#v, want nothing", reqs)
	}
}

// assertGone asserts the CR was released: either garbage-collected by the fake
// client or left without the finalizer.
func assertGone(t *testing.T, cl client.Client, app *v1alpha1.ThunderApplication) {
	t.Helper()
	var updated v1alpha1.ThunderApplication
	err := cl.Get(context.Background(), reqFor(app).NamespacedName, &updated)
	switch {
	case err == nil && containsFinalizer(updated.Finalizers, thunderFinalizer):
		t.Errorf("finalizer still present: %#v", updated.Finalizers)
	case err != nil && !apierrors.IsNotFound(err):
		t.Fatalf("unexpected error getting CR: %v", err)
	}
}

// The owning ResourceReleaseBinding is identified by its own spec fields — a
// binding carries no openchoreo.dev labels to select on.

// A rendered-release-managed app nudges the RenderedRelease that applied it once
// it is ready: the annotation carries the client_id, and a second pass with the
// same id leaves it alone. The binding that owns the app resolves its outputs
// from the RenderedRelease's status snapshot, so that is the object to poke.
func TestReconcile_NudgesRenderedRelease(t *testing.T) {
	app := newApp("dp-ns", "web", v1alpha1.ThunderApplicationSpec{Scopes: "openid"})
	app.Labels[labelRenderedReleaseName] = "r-web-default-abc12345"
	app.Labels[labelRenderedReleaseNamespace] = "org-ns"
	rr := &unstructured.Unstructured{}
	rr.SetGroupVersionKind(schema.GroupVersionKind{Group: "openchoreo.dev", Version: "v1alpha1", Kind: "RenderedRelease"})
	rr.SetNamespace("org-ns")
	rr.SetName("r-web-default-abc12345")

	r, cl := newReconciler(t, &fakeAdmin{}, app, rr)
	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var got v1alpha1.ThunderApplication
	if err := cl.Get(context.Background(), types.NamespacedName{Namespace: "dp-ns", Name: "web"}, &got); err != nil {
		t.Fatalf("get app: %v", err)
	}
	if !got.Status.Ready || got.Status.ClientID == "" {
		t.Fatalf("app not ready with a client_id after reconcile: %+v", got.Status)
	}

	nudged := rr.DeepCopy()
	if err := cl.Get(context.Background(), types.NamespacedName{Namespace: "org-ns", Name: rr.GetName()}, nudged); err != nil {
		t.Fatalf("get RenderedRelease: %v", err)
	}
	if v := nudged.GetAnnotations()[annReadyNudge]; v != got.Status.ClientID {
		t.Fatalf("RenderedRelease nudge = %q, want the client_id %q", v, got.Status.ClientID)
	}
	rv := nudged.GetResourceVersion()

	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if err := cl.Get(context.Background(), types.NamespacedName{Namespace: "org-ns", Name: rr.GetName()}, nudged); err != nil {
		t.Fatalf("get RenderedRelease again: %v", err)
	}
	if nudged.GetResourceVersion() != rv {
		t.Fatalf("a repeat pass with the same client_id must not patch the RenderedRelease again")
	}
}

// An app that no RenderedRelease applied (no rendered-release labels) reconciles
// to ready without looking for one.
func TestReconcile_NoRenderedReleaseLabels_NoNudge(t *testing.T) {
	app := newApp("dp-ns", "web", v1alpha1.ThunderApplicationSpec{Scopes: "openid"})
	r, cl := newReconciler(t, &fakeAdmin{}, app)
	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	var got v1alpha1.ThunderApplication
	if err := cl.Get(context.Background(), types.NamespacedName{Namespace: "dp-ns", Name: "web"}, &got); err != nil {
		t.Fatalf("get app: %v", err)
	}
	if !got.Status.Ready {
		t.Fatalf("app should be ready: %+v", got.Status)
	}
}

// A CR with no (org, environment) labels is a fact about that OBJECT, not
// evidence that the environment's Thunder went away. Both sentinels match, so
// every caller that treats "no binding" as terminal keeps working, while the
// delete path can report the real reason.
func TestResolveTarget_UnlabelledCRIsNoCoordinatesAndNoBinding(t *testing.T) {
	r, _, _ := newReconcilerWithFactory(t, &adminFactory{fixed: &fakeAdmin{}})

	_, err := r.resolveTarget(context.Background(), "", "")
	if err == nil {
		t.Fatal("resolveTarget with no coordinates returned nil error")
	}
	if !errors.Is(err, errNoCoordinates) {
		t.Errorf("error %v does not match errNoCoordinates", err)
	}
	if !errors.Is(err, errNoBinding) {
		t.Errorf("error %v no longer matches errNoBinding — callers treating an absent binding as terminal would change behaviour", err)
	}
}

// The unlabelled-CR delete releases the CR (holding it would wedge the
// namespace) and does not claim the environment lost its Thunder.
func TestReconcile_DeletionOfAnUnlabelledCRReleasesTheCR(t *testing.T) {
	now := metav1.Now()
	app := newApp("ns", "orphan", v1alpha1.ThunderApplicationSpec{Scopes: "openid"})
	app.Labels = nil
	app.DeletionTimestamp = &now
	app.Finalizers = []string{thunderFinalizer}
	app.Status.ClientID = "aep-ns-orphan"
	admin := &fakeAdmin{}
	r, cl := newReconciler(t, admin, app)

	if _, err := r.Reconcile(context.Background(), reqFor(app)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(admin.deleteCalls) != 0 {
		t.Errorf("DeleteApplication called %#v — there is no instance to call", admin.deleteCalls)
	}

	var updated v1alpha1.ThunderApplication
	err := cl.Get(context.Background(), reqFor(app).NamespacedName, &updated)
	if err == nil && containsFinalizer(updated.Finalizers, thunderFinalizer) {
		t.Error("finalizer still present — an unlabelled CR would wedge its namespace forever")
	} else if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("unexpected error getting CR: %v", err)
	}
}
