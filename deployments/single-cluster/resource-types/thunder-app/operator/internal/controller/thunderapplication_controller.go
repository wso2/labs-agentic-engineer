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

// Package controller reconciles ThunderApplication custom resources into
// OAuth clients on the platform Thunder IdP, publishing the assigned
// client_id back into the cluster as a ConfigMap and gating readiness via
// the CR's status subresource.
package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/wso2/aep/thunder-app-operator/api/v1alpha1"
	"github.com/wso2/aep/thunder-app-operator/internal/thunder"
)

const (
	// thunderFinalizer guards a ThunderApplication so its backing Thunder app
	// is removed before the CR disappears.
	thunderFinalizer = "aep.wso2.com/thunder-application"

	// oauthConfigMapSuffix is appended to the CR name for its published
	// client_id ConfigMap (e.g. "my-app" -> "my-app-oauth").
	oauthConfigMapSuffix = "-oauth"

	// errBackoff is the fixed requeue delay after a Thunder failure. Modest
	// and constant on purpose: Thunder outages are transient and the CR is
	// cheap to re-reconcile; exponential backoff is not worth the complexity
	// at v1 scope.
	errBackoff = 30 * time.Second

	// Labels OpenChoreo's renderedrelease-controller stamps on every rendered
	// object — they let us trace this app back to the owning ResourceReleaseBinding.
	labelResource     = "openchoreo.dev/resource"
	labelCPNamespace  = "openchoreo.dev/namespace"
	// annReadyNudge, set on the owning binding, forces one binding re-reconcile
	// when this app becomes ready (see nudgeOwningBindings).
	annReadyNudge = "aep.wso2.com/thunder-ready-nudge"
)

// Reconciler reconciles ThunderApplication objects.
type Reconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Thunder thunder.AdminClient
}

// +kubebuilder:rbac:groups=aep.wso2.com,resources=thunderapplications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=aep.wso2.com,resources=thunderapplications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=aep.wso2.com,resources=thunderapplications/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=openchoreo.dev,resources=resourcereleasebindings,verbs=get;list;patch

// Reconcile drives a ThunderApplication toward its desired Thunder OAuth
// application and publishes the resulting client_id.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var app v1alpha1.ThunderApplication
	if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !app.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &app)
	}

	// Ensure the finalizer is in place, then continue in-band: the local
	// object now carries the finalizer, so a requeue is not required to make
	// forward progress this pass.
	if controllerutil.AddFinalizer(&app, thunderFinalizer) {
		if err := r.Update(ctx, &app); err != nil {
			return ctrl.Result{}, err
		}
		// Re-fetch so Status().Update below uses the post-finalizer ResourceVersion.
		if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
	}

	desired, err := r.buildDesiredApp(ctx, &app)
	if err != nil {
		logger.Error(err, "buildDesiredApp failed", "thunderApp", clientIDForApp(&app))
		return r.markError(ctx, &app, err)
	}

	clientID, err := r.Thunder.EnsureApplication(ctx, desired)
	if err != nil {
		logger.Error(err, "EnsureApplication failed", "thunderApp", clientIDForApp(&app))
		return r.markError(ctx, &app, err)
	}

	if err := r.ensureConfigMap(ctx, &app, clientID); err != nil {
		logger.Error(err, "publish oauth ConfigMap failed")
		return r.markError(ctx, &app, err)
	}

	app.Status.Ready = true
	app.Status.ClientID = clientID
	app.Status.Message = ""
	app.Status.ObservedGeneration = app.Generation
	if err := r.Status().Update(ctx, &app); err != nil {
		return ctrl.Result{}, err
	}
	r.nudgeOwningBindings(ctx, &app)
	return ctrl.Result{}, nil
}

// nudgeOwningBindings forces OpenChoreo's ResourceReleaseBinding controller to
// re-reconcile the binding(s) that rendered this app. That controller evaluates
// its readyWhen/outputs CEL against the app's status ONCE — before Thunder has
// minted the client_id — and does not requeue when the app later becomes ready,
// so the binding stays Ready=False forever and any build depending on the
// resource hangs. Patching an annotation triggers exactly one binding
// reconcile, which now sees status.ready + status.clientId and resolves its
// outputs. The nudge value is the client_id (stable), so repeat passes with the
// same id are skipped — no reconcile storm. Best-effort: a failure only leaves
// the pre-existing stuck state, which the next reconcile retries.
func (r *Reconciler) nudgeOwningBindings(ctx context.Context, app *v1alpha1.ThunderApplication) {
	logger := log.FromContext(ctx)
	resource := app.Labels[labelResource]
	cpNamespace := app.Labels[labelCPNamespace]
	if resource == "" || cpNamespace == "" {
		return // not a rendered-release-managed app — nothing owns it
	}

	bindings := &unstructured.UnstructuredList{}
	bindings.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "openchoreo.dev",
		Version: "v1alpha1",
		Kind:    "ResourceReleaseBindingList",
	})
	if err := r.List(ctx, bindings,
		client.InNamespace(cpNamespace),
		client.MatchingLabels{labelResource: resource},
	); err != nil {
		logger.Error(err, "nudge: list owning bindings failed", "resource", resource)
		return
	}

	for i := range bindings.Items {
		b := &bindings.Items[i]
		anns := b.GetAnnotations()
		if anns[annReadyNudge] == app.Status.ClientID {
			continue // already nudged for this client_id
		}
		before := b.DeepCopy()
		if anns == nil {
			anns = map[string]string{}
		}
		anns[annReadyNudge] = app.Status.ClientID
		b.SetAnnotations(anns)
		if err := r.Patch(ctx, b, client.MergeFrom(before)); err != nil {
			logger.Error(err, "nudge: patch binding failed", "binding", b.GetName())
		}
	}
}

// reconcileDelete removes the backing Thunder app and drops the finalizer.
// The published ConfigMap is garbage-collected via its owner reference.
func (r *Reconciler) reconcileDelete(ctx context.Context, app *v1alpha1.ThunderApplication) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(app, thunderFinalizer) {
		return ctrl.Result{}, nil
	}

	if err := r.Thunder.DeleteApplication(ctx, clientIDForApp(app)); err != nil {
		log.FromContext(ctx).Error(err, "DeleteApplication failed", "thunderApp", clientIDForApp(app))
		return r.markError(ctx, app, err)
	}

	controllerutil.RemoveFinalizer(app, thunderFinalizer)
	if err := r.Update(ctx, app); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// markError records a Thunder failure on the CR status and requeues after a
// fixed backoff. It deliberately returns a nil error alongside RequeueAfter:
// controller-runtime forbids returning both a non-nil error and RequeueAfter,
// and the RequeueAfter path is what lets the message reach status.
func (r *Reconciler) markError(ctx context.Context, app *v1alpha1.ThunderApplication, cause error) (ctrl.Result, error) {
	base := app.DeepCopy()
	app.Status.Ready = false
	app.Status.Message = cause.Error()
	if err := r.Status().Patch(ctx, app, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: errBackoff}, nil
}

// ensureConfigMap creates or refreshes the <cr-name>-oauth ConfigMap carrying
// the assigned client_id, owned (controller ref) by the CR so it cascades on
// delete.
func (r *Reconciler) ensureConfigMap(ctx context.Context, app *v1alpha1.ThunderApplication, clientID string) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name + oauthConfigMapSuffix,
			Namespace: app.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		cm.Data["client_id"] = clientID
		return controllerutil.SetControllerReference(app, cm, r.Scheme)
	})
	return err
}

// buildDesiredApp derives the desired Thunder app state from a CR. For
// confidential clients it reads the client secret from the referenced Secret.
func (r *Reconciler) buildDesiredApp(ctx context.Context, app *v1alpha1.ThunderApplication) (thunder.DesiredApp, error) {
	switch app.Spec.ClientType {
	case "", "public", "confidential":
		// valid
	default:
		return thunder.DesiredApp{}, fmt.Errorf("unsupported clientType %q — must be \"public\" or \"confidential\"", app.Spec.ClientType)
	}
	desired := desiredApp(app)
	if app.Spec.ClientType == "confidential" {
		if app.Spec.SecretRef == nil {
			return thunder.DesiredApp{}, fmt.Errorf("confidential client %q requires spec.secretRef", app.Name)
		}
		var sec corev1.Secret
		if err := r.Get(ctx, types.NamespacedName{Name: app.Spec.SecretRef.Name, Namespace: app.Namespace}, &sec); err != nil {
			return thunder.DesiredApp{}, fmt.Errorf("read secretRef %s/%s: %w", app.Namespace, app.Spec.SecretRef.Name, err)
		}
		desired.ClientSecret = string(sec.Data[app.Spec.SecretRef.Key])
		if desired.ClientSecret == "" {
			return thunder.DesiredApp{}, fmt.Errorf("secret %s/%s key %q is empty", app.Namespace, app.Spec.SecretRef.Name, app.Spec.SecretRef.Key)
		}
	}
	return desired, nil
}

// desiredApp derives the Thunder desired state from a CR (without secret lookup).
func desiredApp(app *v1alpha1.ThunderApplication) thunder.DesiredApp {
	displayName := app.Spec.DisplayName
	if displayName == "" {
		displayName = app.Name
	}
	return thunder.DesiredApp{
		Name:         clientIDForApp(app),
		DisplayName:  displayName,
		Scopes:       strings.Fields(app.Spec.Scopes),
		RedirectURIs: splitRedirectURIs(app.Spec.RedirectURIs),
		ClientType:   app.Spec.ClientType,
	}
}

// clientIDForApp returns the Thunder client ID for a CR: spec.clientId when
// set (explicit override), otherwise the derived aep-<namespace>-<name>.
func clientIDForApp(app *v1alpha1.ThunderApplication) string {
	if app.Spec.ClientID != "" {
		return app.Spec.ClientID
	}
	return thunderAppName(app)
}

// thunderAppName is the derived Thunder application identity (fallback when
// spec.clientId is not set).
func thunderAppName(app *v1alpha1.ThunderApplication) string {
	return fmt.Sprintf("aep-%s-%s", app.Namespace, app.Name)
}

// splitRedirectURIs splits the comma-separated RedirectURIs spec field into a
// clean slice: empty input yields an empty slice, and stray commas/whitespace
// never produce empty elements.
func splitRedirectURIs(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// SetupWithManager wires the reconciler to watch ThunderApplications, the
// ConfigMaps it owns, and Secrets referenced by confidential clients so that
// secret rotation triggers an immediate re-reconcile.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ThunderApplication{}).
		Owns(&corev1.ConfigMap{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.secretToThunderApps)).
		Named("thunderapplication").
		Complete(r)
}

// secretToThunderApps maps a Secret change to the ThunderApplications in the
// same namespace that reference it via spec.secretRef, so secret rotation
// immediately re-queues affected CRs.
func (r *Reconciler) secretToThunderApps(ctx context.Context, obj client.Object) []reconcile.Request {
	var appList v1alpha1.ThunderApplicationList
	if err := r.List(ctx, &appList, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}
	var reqs []reconcile.Request
	for _, app := range appList.Items {
		if app.Spec.SecretRef != nil && app.Spec.SecretRef.Name == obj.GetName() {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: app.Namespace,
					Name:      app.Name,
				},
			})
		}
	}
	return reqs
}
