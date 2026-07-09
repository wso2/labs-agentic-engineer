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
	"k8s.io/apimachinery/pkg/runtime"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

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
	}

	clientID, err := r.Thunder.EnsureApplication(ctx, desiredApp(&app))
	if err != nil {
		logger.Error(err, "EnsureApplication failed", "thunderApp", thunderAppName(&app))
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
	return ctrl.Result{}, nil
}

// reconcileDelete removes the backing Thunder app and drops the finalizer.
// The published ConfigMap is garbage-collected via its owner reference.
func (r *Reconciler) reconcileDelete(ctx context.Context, app *v1alpha1.ThunderApplication) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(app, thunderFinalizer) {
		return ctrl.Result{}, nil
	}

	if err := r.Thunder.DeleteApplication(ctx, thunderAppName(app)); err != nil {
		log.FromContext(ctx).Error(err, "DeleteApplication failed", "thunderApp", thunderAppName(app))
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
	app.Status.Ready = false
	app.Status.Message = cause.Error()
	if err := r.Status().Update(ctx, app); err != nil {
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

// desiredApp derives the Thunder desired state from a CR.
func desiredApp(app *v1alpha1.ThunderApplication) thunder.DesiredApp {
	displayName := app.Spec.DisplayName
	if displayName == "" {
		displayName = app.Name
	}
	return thunder.DesiredApp{
		Name:         thunderAppName(app),
		DisplayName:  displayName,
		Scopes:       strings.Fields(app.Spec.Scopes),
		RedirectURIs: splitRedirectURIs(app.Spec.RedirectURIs),
	}
}

// thunderAppName is the deterministic per-CR Thunder application identity.
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

// SetupWithManager wires the reconciler to watch ThunderApplications and the
// ConfigMaps it owns.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ThunderApplication{}).
		Owns(&corev1.ConfigMap{}).
		Named("thunderapplication").
		Complete(r)
}
