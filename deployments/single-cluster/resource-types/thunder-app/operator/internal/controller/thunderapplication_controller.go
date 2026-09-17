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
// OAuth clients on the Thunder instance that serves the CR's (org,
// environment) — resolved per CR from the binding record, see binding.go —
// publishing the assigned client_id and the instance's issuer back into the
// cluster as a ConfigMap and gating readiness via the CR's status subresource.
package controller

import (
	"context"
	"errors"
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
	// at v1 scope. A missing binding uses the same delay — the environment's
	// Thunder is provisioned by a script that also writes the binding, so the
	// wait is "until someone runs it", and a change to either half re-queues
	// the CR immediately anyway (see SetupWithManager).
	errBackoff = 30 * time.Second

	// Labels OpenChoreo's renderedrelease-controller stamps on every rendered
	// object. resource + namespace let us trace an app back to the owning
	// ResourceReleaseBinding; namespace (the OC namespace = the org) and
	// environment are the (org, env) coordinates the Thunder binding is keyed
	// by. Both are also the label pair aep-api's clients/thunderapp reads.
	labelCPNamespace = "openchoreo.dev/namespace"
	labelEnvironment = "openchoreo.dev/environment"

	// labelRenderedReleaseName / labelRenderedReleaseNamespace name the
	// RenderedRelease that applied this app — stamped by OpenChoreo's
	// renderedrelease-controller beside the labels above (see nudgeRenderedRelease).
	labelRenderedReleaseName      = "openchoreo.dev/rendered-release-name"
	labelRenderedReleaseNamespace = "openchoreo.dev/rendered-release-namespace"

	// annReadyNudge, set on the RenderedRelease that applied this app, forces
	// one re-observation of the app's status when it becomes ready (see
	// nudgeRenderedRelease).
	annReadyNudge = "aep.wso2.com/thunder-ready-nudge"
)

// Reconciler reconciles ThunderApplication objects.
type Reconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// PodNamespace is the operator's own namespace — the only namespace it can
	// read Secrets from, and therefore where every binding Secret is mirrored.
	PodNamespace string
	// NewThunderClient builds an admin client for one resolved target. nil
	// means thunder.New; tests substitute a recorder.
	NewThunderClient func(thunder.Config) thunder.AdminClient

	cache clientCache
}

// +kubebuilder:rbac:groups=aep.wso2.com,resources=thunderapplications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=aep.wso2.com,resources=thunderapplications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=aep.wso2.com,resources=thunderapplications/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=openchoreo.dev,resources=renderedreleases,verbs=get;patch

// Reconcile drives a ThunderApplication toward its desired Thunder OAuth
// application on the instance that serves its (org, environment), and
// publishes the resulting client_id and issuer.
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

	org, env := orgEnvOf(&app)
	target, err := r.resolveTarget(ctx, org, env)
	if err != nil {
		// Not an operator fault and not a Thunder outage: the environment has
		// no Thunder yet, or its record is incomplete. The message names what
		// is missing so `kubectl describe` is enough to fix it.
		logger.Info("thunder binding unresolved", "org", org, "environment", env, "reason", err.Error())
		return r.markError(ctx, &app, err)
	}

	desired, err := r.buildDesiredApp(ctx, &app)
	if err != nil {
		logger.Error(err, "buildDesiredApp failed", "thunderApp", clientIDForApp(&app))
		return r.markError(ctx, &app, err)
	}

	clientID, err := r.clientFor(target).EnsureApplication(ctx, desired)
	if err != nil {
		logger.Error(err, "EnsureApplication failed",
			"thunderApp", clientIDForApp(&app), "issuer", target.Issuer)
		return r.markError(ctx, &app, err)
	}

	if err := r.ensureConfigMap(ctx, &app, clientID, target); err != nil {
		logger.Error(err, "publish oauth ConfigMap failed")
		return r.markError(ctx, &app, err)
	}

	// Deliberately not fatal to readiness: the OAuth app exists and every
	// consumer of this CR's outputs can proceed. What is missing is only the
	// browser's permission to CALL the IdP, so failing the CR here would take
	// a whole deployment down for a transient IdP hiccup. The reason lands on
	// status.Message and the CR requeues until it sticks.
	originsErr := r.syncBrowserOrigins(ctx, target, org, env)
	if originsErr != nil {
		logger.Error(originsErr, "sync browser origins failed — the app's SPA cannot call the IdP until this succeeds",
			"thunderApp", clientIDForApp(&app), "issuer", target.Issuer)
	}

	app.Status.Ready = true
	app.Status.ClientID = clientID
	app.Status.Issuer = target.Issuer
	app.Status.JWKSURL = target.jwksURL()
	app.Status.AdminURL = target.AdminURL
	app.Status.Message = ""
	if originsErr != nil {
		app.Status.Message = "browser origins not registered: " + originsErr.Error()
	}
	app.Status.ObservedGeneration = app.Generation
	if err := r.Status().Update(ctx, &app); err != nil {
		return ctrl.Result{}, err
	}
	r.nudgeRenderedRelease(ctx, &app)
	if originsErr != nil {
		return ctrl.Result{RequeueAfter: errBackoff}, nil
	}
	return ctrl.Result{}, nil
}

// syncBrowserOrigins projects every ThunderApplication rendered into (org, env)
// onto the writable CORS layer of the Thunder that serves them.
//
// The set is recomputed from the CRs on every pass rather than appended to,
// which is what makes deletes and drift converge without the operator keeping
// any bookkeeping of its own (see thunder.SetBrowserOrigins). Apps under
// deletion are excluded: their origin is on its way out, and the CR that is
// finalizing calls this again after its app is gone.
func (r *Reconciler) syncBrowserOrigins(ctx context.Context, target thunderTarget, org, env string) error {
	var apps v1alpha1.ThunderApplicationList
	if err := r.List(ctx, &apps, client.MatchingLabels{
		labelCPNamespace: org,
		labelEnvironment: env,
	}); err != nil {
		return fmt.Errorf("list thunder applications for org=%s env=%s: %w", org, env, err)
	}

	// A set: several apps legitimately share an origin (one SPA whose service
	// declares the same auth dependency), and several redirect URIs on one app
	// collapse onto one.
	seen := map[string]struct{}{}
	var origins []string
	for i := range apps.Items {
		peer := &apps.Items[i]
		if !peer.DeletionTimestamp.IsZero() {
			continue
		}
		for _, uri := range splitRedirectURIs(peer.Spec.RedirectURIs) {
			origin := thunder.OriginOf(uri)
			if origin == "" {
				continue
			}
			if _, dup := seen[origin]; dup {
				continue
			}
			seen[origin] = struct{}{}
			origins = append(origins, origin)
		}
	}
	return r.clientFor(target).SetBrowserOrigins(ctx, origins)
}

// nudgeRenderedRelease makes OpenChoreo re-read this app's status now rather
// than on its next stable requeue.
//
// The ResourceReleaseBinding that owns this app resolves its `issuer` and
// `jwks_url` outputs from `applied.app.status.*` — but not from the live CR.
// OpenChoreo's binding controller evaluates the CEL against the STATUS SNAPSHOT
// the RenderedRelease controller records in `RenderedRelease.status.resources[]`
// when it applies the manifests, and that controller does not watch the objects
// it applied: it re-observes them only on its own requeue (about six minutes
// on OpenChoreo 1.2). The apply and the operator's status write race, so the
// snapshot is usually taken before `status.issuer` exists, the binding reports
// OutputResolutionFailed, and every Thunder-app provision waits out the requeue
// (measured live: Ready flipped at exactly the requeue, six minutes after the
// CR was ready). Nudging the BINDING cannot help — it would only re-evaluate
// the same stale snapshot.
//
// Patching an annotation on the RenderedRelease triggers one reconcile of the
// controller that owns the snapshot: it re-reads the live CR, records the
// status, and the binding — which Owns the RenderedRelease — re-reconciles off
// that status update and resolves its outputs. The nudge value is the client_id
// (stable), so repeat passes with the same id are no-ops. Best-effort: a
// failure only leaves the pre-existing delay, which the requeue ends.
//
// The RenderedRelease is found by the name/namespace labels
// renderedrelease-controller stamps on every object it applies.
func (r *Reconciler) nudgeRenderedRelease(ctx context.Context, app *v1alpha1.ThunderApplication) {
	logger := log.FromContext(ctx)
	name := app.Labels[labelRenderedReleaseName]
	namespace := app.Labels[labelRenderedReleaseNamespace]
	if name == "" || namespace == "" {
		return // not a rendered-release-managed app — nothing snapshots it
	}

	rr := &unstructured.Unstructured{}
	rr.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "openchoreo.dev",
		Version: "v1alpha1",
		Kind:    "RenderedRelease",
	})
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, rr); err != nil {
		logger.Error(err, "nudge: get RenderedRelease failed", "renderedRelease", namespace+"/"+name)
		return
	}
	anns := rr.GetAnnotations()
	if anns[annReadyNudge] == app.Status.ClientID {
		return // already nudged for this client_id
	}
	before := rr.DeepCopy()
	if anns == nil {
		anns = map[string]string{}
	}
	anns[annReadyNudge] = app.Status.ClientID
	rr.SetAnnotations(anns)
	if err := r.Patch(ctx, rr, client.MergeFrom(before)); err != nil {
		logger.Error(err, "nudge: patch RenderedRelease failed", "renderedRelease", namespace+"/"+name)
	}
}

// reconcileDelete removes the backing Thunder app and drops the finalizer.
// The published ConfigMap is garbage-collected via its owner reference.
//
// The app has to be removed from the SAME instance it was created on, and the
// credential for any instance lives only in that instance's binding Secret. So
// the delete takes one of two paths:
//
//   - the binding resolves and still points at the instance recorded on status
//     (or the CR never got that far) — delete there, the normal path;
//   - the binding is unusable — see below.
//
// "Unusable" splits in two, and the split is the whole point. A binding
// ConfigMap that exists but whose Secret is missing or incomplete is
// REPAIRABLE: the instance is still there, the credential can be re-mirrored by
// re-running setup-environment-thunder.sh, so the delete keeps the finalizer
// and retries. A binding ConfigMap that is ABSENT (or now names a different
// instance) means the (org, env) no longer has the Thunder this app was
// registered on — remove-environment-thunder.sh deletes the record together
// with the release — so there is nothing left to delete from and no credential
// that could ever reach it. Holding the finalizer then would wedge the CR and
// its namespace forever, so the operator logs the orphaned client_id and issuer
// at error level and lets the CR go.
func (r *Reconciler) reconcileDelete(ctx context.Context, app *v1alpha1.ThunderApplication) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(app, thunderFinalizer) {
		return ctrl.Result{}, nil
	}
	logger := log.FromContext(ctx)
	name := clientIDForApp(app)

	org, env := orgEnvOf(app)
	target, err := r.resolveTarget(ctx, org, env)
	switch {
	case errors.Is(err, errNoCoordinates):
		// The CR cannot say where it belongs, so nothing can be looked up —
		// but that is a fact about THIS object, not about the environment. Its
		// Thunder may be alive and still holding the app, which is why the
		// coordinates are reported instead of "the environment has no Thunder".
		if app.Status.ClientID == "" {
			logger.Info("no (org, environment) labels and nothing registered — releasing the CR",
				"thunderApp", name)
			return r.dropFinalizer(ctx, app)
		}
		logger.Error(err, "deleting a ThunderApplication that carries no (org, environment) labels — "+
			"the application is left behind on the instance it was registered on",
			"thunderApp", name, "issuer", app.Status.Issuer, "adminURL", app.Status.AdminURL)
		return r.dropFinalizer(ctx, app)
	case errors.Is(err, errNoBinding):
		if app.Status.ClientID == "" {
			// Nothing was ever registered, so nothing is left behind.
			logger.Info("no thunder binding and nothing registered — releasing the CR", "thunderApp", name)
			return r.dropFinalizer(ctx, app)
		}
		logger.Error(err, "deleting a ThunderApplication whose environment has no Thunder binding — "+
			"the application is left behind if that instance still exists",
			"thunderApp", name, "issuer", app.Status.Issuer, "adminURL", app.Status.AdminURL)
		return r.dropFinalizer(ctx, app)
	case err != nil:
		// Repairable: the record is there, the credential is not.
		logger.Info("delete deferred — thunder binding unusable", "thunderApp", name, "reason", err.Error())
		return r.markError(ctx, app, err)
	case app.Status.AdminURL != "" && target.AdminURL != app.Status.AdminURL:
		logger.Error(nil, "the environment's Thunder binding now names a different instance than the one this "+
			"application was registered on — the application is left behind on the old instance",
			"thunderApp", name, "registeredOn", app.Status.AdminURL, "bindingNames", target.AdminURL)
		return r.dropFinalizer(ctx, app)
	}

	if err := r.clientFor(target).DeleteApplication(ctx, name); err != nil {
		logger.Error(err, "DeleteApplication failed", "thunderApp", name, "issuer", target.Issuer)
		return r.markError(ctx, app, err)
	}

	// Withdraw this app's origin. Best-effort on purpose: holding the CR back
	// over a stale entry in an allow-list would wedge the namespace, and the
	// entry is corrected by the next reconcile of any sibling app on the same
	// instance. An instance whose last app just went away has no such sibling,
	// so the failure is logged loudly enough to be found.
	if err := r.syncBrowserOrigins(ctx, target, org, env); err != nil {
		logger.Error(err, "the deleted app's browser origin is left in the IdP's CORS allow-list",
			"thunderApp", name, "issuer", target.Issuer)
	}
	return r.dropFinalizer(ctx, app)
}

// dropFinalizer releases the CR.
func (r *Reconciler) dropFinalizer(ctx context.Context, app *v1alpha1.ThunderApplication) (ctrl.Result, error) {
	controllerutil.RemoveFinalizer(app, thunderFinalizer)
	if err := r.Update(ctx, app); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// markError records a failure on the CR status and requeues after a fixed
// backoff. It deliberately returns a nil error alongside RequeueAfter:
// controller-runtime forbids returning both a non-nil error and RequeueAfter,
// and the RequeueAfter path is what lets the message reach status.
//
// Update, not Patch: `ready` is a REQUIRED property of the status schema, and a
// merge patch carries only the fields that changed. On a CR that has never had
// a status — the first pass of a CR whose environment has no Thunder yet — the
// patch would write a status object with a message and no `ready`, which the
// API server rejects ("status.ready: Required value"). The reconcile then
// returns an error instead of the backoff, and the CR spins.
func (r *Reconciler) markError(ctx context.Context, app *v1alpha1.ThunderApplication, cause error) (ctrl.Result, error) {
	app.Status.Ready = false
	app.Status.Message = cause.Error()
	if err := r.Status().Update(ctx, app); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: errBackoff}, nil
}

// ensureConfigMap creates or refreshes the <cr-name>-oauth ConfigMap carrying
// the assigned client_id and the coordinates of the Thunder instance it lives
// on, owned (controller ref) by the CR so it cascades on delete. The issuer is
// here as well as on status because a workload consuming this resource reads
// the ConfigMap, not the CR.
func (r *Reconciler) ensureConfigMap(
	ctx context.Context, app *v1alpha1.ThunderApplication, clientID string, target thunderTarget,
) error {
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
		cm.Data["issuer"] = target.Issuer
		cm.Data["jwks_url"] = target.jwksURL()
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
		Name:        clientIDForApp(app),
		DisplayName: displayName,
		// The CR carries the scope set space-joined (that is the shape the
		// ClusterResourceType parameter has); ThunderID's application contract
		// types the field as a JSON array, so the split happens here.
		Scopes:         strings.Fields(app.Spec.Scopes),
		ValidityPeriod: app.Spec.ValidityPeriod,
		RedirectURIs:   splitRedirectURIs(app.Spec.RedirectURIs),
		ClientType:     app.Spec.ClientType,
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
// ConfigMaps it owns, the binding ConfigMaps that say where each (org, env)'s
// Thunder is, and Secrets — both the credential half of a binding and the
// Secrets referenced by confidential clients — so that a re-mirrored or rotated
// credential triggers an immediate re-reconcile instead of waiting out the
// error backoff.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ThunderApplication{}).
		Owns(&corev1.ConfigMap{}).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(r.bindingToThunderApps)).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.secretToThunderApps)).
		Named("thunderapplication").
		Complete(r)
}

// bindingToThunderApps maps a change to a binding record — either half — to
// every ThunderApplication in that (org, environment). Objects without the
// binding labels are ignored, which is what keeps the operator's own -oauth
// ConfigMaps (and every other ConfigMap in the cluster) out of the queue.
func (r *Reconciler) bindingToThunderApps(ctx context.Context, obj client.Object) []reconcile.Request {
	labels := obj.GetLabels()
	if labels[labelBindingKind] != bindingKind {
		return nil
	}
	org, env := labels[labelBindingOrg], labels[labelBindingEnv]
	if org == "" || env == "" {
		return nil
	}
	var appList v1alpha1.ThunderApplicationList
	if err := r.List(ctx, &appList, client.MatchingLabels{
		labelCPNamespace: org,
		labelEnvironment: env,
	}); err != nil {
		return nil
	}
	reqs := make([]reconcile.Request, 0, len(appList.Items))
	for i := range appList.Items {
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: appList.Items[i].Namespace,
				Name:      appList.Items[i].Name,
			},
		})
	}
	return reqs
}

// secretToThunderApps maps a Secret change to the ThunderApplications it
// affects: the CRs in the same namespace that reference it via spec.secretRef
// (client-secret rotation) and, when the Secret is the credential half of a
// binding record, every CR in that (org, environment).
func (r *Reconciler) secretToThunderApps(ctx context.Context, obj client.Object) []reconcile.Request {
	reqs := r.bindingToThunderApps(ctx, obj)

	var appList v1alpha1.ThunderApplicationList
	if err := r.List(ctx, &appList, client.InNamespace(obj.GetNamespace())); err != nil {
		return reqs
	}
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
