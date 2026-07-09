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
	clientID    string
	ensureErr   error
	deleteErr   error
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

func newReconciler(t *testing.T, admin thunder.AdminClient, objs ...client.Object) (*Reconciler, client.Client) {
	t.Helper()
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.ThunderApplication{}).
		WithObjects(objs...).
		Build()
	return &Reconciler{Client: cl, Scheme: scheme, Thunder: admin}, cl
}

func reqFor(app *v1alpha1.ThunderApplication) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Namespace: app.Namespace, Name: app.Name}}
}

func newApp(ns, name string, spec v1alpha1.ThunderApplicationSpec) *v1alpha1.ThunderApplication {
	return &v1alpha1.ThunderApplication{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  ns,
			Name:       name,
			Generation: 1,
		},
		Spec: spec,
	}
}

// (a) Fresh CR: finalizer added, EnsureApplication called with derived name +
// split scopes/redirectURIs, ConfigMap published with owner ref, status ready.
func TestReconcile_FreshCR(t *testing.T) {
	app := newApp("test-ns", "my-app", v1alpha1.ThunderApplicationSpec{
		DisplayName:  "My App",
		Scopes:       "openid profile email",
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
	if !reflect.DeepEqual(got.Scopes, []string{"openid", "profile", "email"}) {
		t.Errorf("Scopes = %#v, want [openid profile email]", got.Scopes)
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

func containsFinalizer(finalizers []string, want string) bool {
	for _, f := range finalizers {
		if f == want {
			return true
		}
	}
	return false
}
