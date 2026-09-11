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
	"fmt"
	"sort"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/wso2/aep/thunder-app-operator/api/v1alpha1"
	"github.com/wso2/aep/thunder-app-operator/internal/thunder"
)

// The binding record written by deployments/scripts/setup-environment-thunder.sh
// for every (org, environment) that has its own Thunder. It is delivered in two
// halves, because no single object can be read by all three of its consumers:
//
//	ConfigMap  <any name>, in the environment-Thunder's own namespace, carrying
//	           the non-secret coordinates. Selected by LABEL, never by name.
//	Secret     named by the ConfigMap's `secretName` key, mirrored into the
//	           operator's OWN namespace, carrying the credential.
//
// The operator's Secret informer and RBAC are deliberately restricted to its own
// namespace (see main.go), so the mirror is how a credential reaches it at all.
// The ConfigMap informer is cluster-wide, so the non-secret half is read where it
// is written.
const (
	labelBindingKind = "aep.wso2.com/kind"
	labelBindingOrg  = "aep.wso2.com/org"
	labelBindingEnv  = "aep.wso2.com/env"
	// bindingKind is the value of labelBindingKind that marks a binding record.
	bindingKind = "thunder-binding"

	// Keys of the binding ConfigMap this operator reads. The record carries
	// more (trusted-issuer triple, OpenBao path, release name); those are for
	// the other consumers.
	keyIssuer          = "issuer"
	keyAdminURL        = "adminURL"
	keySystemResource  = "systemResourceIdentifier"
	keySecretName      = "secretName"
	keySecretNamespace = "secretNamespace"

	// Keys of the mirrored binding Secret.
	keyClientID     = "client-id"
	keyClientSecret = "client-secret"
)

// errNoBinding reports that no binding ConfigMap exists for an (org, env) at
// all — as opposed to one that exists but is unusable (missing Secret, missing
// key). The delete path treats the two differently: an absent binding means the
// environment has no Thunder to delete from, while a broken one is repairable
// and worth waiting for. Wrap with %w; test with errors.Is.
var errNoBinding = errors.New("no thunder binding")

// errNoCoordinates reports the narrower case where the CR itself does not say
// which (org, environment) it belongs to, so no binding could be looked up in
// the first place. It is wrapped ALONGSIDE errNoBinding — every caller that
// treats "no binding" as terminal still does — but the delete path reports it
// differently: an unlabelled CR is not evidence that the environment's Thunder
// went away, and saying so would send the reader looking for a removed
// instance instead of at the missing labels.
var errNoCoordinates = errors.New("no (org, environment) on the CR")

// thunderTarget is one resolved Thunder instance: where it is, how to
// authenticate to it, and which record it came from. Comparable on purpose —
// the per-target client cache invalidates by comparing the whole struct, so any
// change to the ConfigMap or the Secret rebuilds the client without needing a
// separate invalidation path.
type thunderTarget struct {
	Org string
	Env string
	// Issuer is the browser-reachable OIDC issuer of the instance, published
	// on the CR's status and in its -oauth ConfigMap.
	Issuer string
	// AdminURL is the in-cluster admin API base the operator writes to.
	AdminURL string
	// SystemResourceIdentifier is the OAuth resource indicator for the
	// instance's own System resource server (see thunder.Config).
	SystemResourceIdentifier string
	// BindingName / BindingNamespace name the ConfigMap this came from —
	// carried for error messages only.
	BindingName      string
	BindingNamespace string
	ClientID         string
	ClientSecret     string
}

// jwksURL is the instance's JWKS endpoint, derived from the issuer the way
// every OIDC consumer derives it.
func (t thunderTarget) jwksURL() string {
	return strings.TrimSuffix(t.Issuer, "/") + "/oauth2/jwks"
}

// bindingKeyOf is the cache key for a target: one Thunder per (org, env).
func bindingKeyOf(org, env string) string { return org + "/" + env }

// orgEnvOf reads the (org, environment) coordinates OpenChoreo's
// renderedrelease-controller stamps on every object it renders. They are the
// only thing that tells this operator which Thunder a CR belongs to.
func orgEnvOf(app *v1alpha1.ThunderApplication) (org, env string) {
	return app.Labels[labelCPNamespace], app.Labels[labelEnvironment]
}

// resolveTarget finds the Thunder instance serving (org, env): the binding
// ConfigMap by label, then the Secret it names in the operator's own namespace.
func (r *Reconciler) resolveTarget(ctx context.Context, org, env string) (thunderTarget, error) {
	if org == "" || env == "" {
		return thunderTarget{}, fmt.Errorf(
			"%w (%w): cannot resolve one without an (org, environment) — the CR carries no %s/%s labels, "+
				"which OpenChoreo stamps on every rendered object",
			errNoBinding, errNoCoordinates, labelCPNamespace, labelEnvironment)
	}

	var cms corev1.ConfigMapList
	if err := r.List(ctx, &cms, client.MatchingLabels{
		labelBindingKind: bindingKind,
		labelBindingOrg:  org,
		labelBindingEnv:  env,
	}); err != nil {
		return thunderTarget{}, fmt.Errorf("list thunder bindings for org=%s env=%s: %w", org, env, err)
	}

	switch len(cms.Items) {
	case 0:
		return thunderTarget{}, fmt.Errorf(
			"%w for org=%s env=%s: expected a ConfigMap labelled %s=%s,%s=%s,%s=%s — "+
				"run deployments/scripts/setup-environment-thunder.sh %s %s",
			errNoBinding, org, env,
			labelBindingKind, bindingKind, labelBindingOrg, org, labelBindingEnv, env, org, env)
	case 1:
		// the one good case
	default:
		return thunderTarget{}, fmt.Errorf(
			"ambiguous thunder binding for org=%s env=%s: %s — exactly one ConfigMap may carry these labels",
			org, env, strings.Join(namespacedNames(cms.Items), ", "))
	}

	cm := cms.Items[0]
	from := cm.Namespace + "/" + cm.Name
	tgt := thunderTarget{
		Org:                      org,
		Env:                      env,
		Issuer:                   cm.Data[keyIssuer],
		AdminURL:                 cm.Data[keyAdminURL],
		SystemResourceIdentifier: cm.Data[keySystemResource],
		BindingName:              cm.Name,
		BindingNamespace:         cm.Namespace,
	}
	for key, value := range map[string]string{keyIssuer: tgt.Issuer, keyAdminURL: tgt.AdminURL} {
		if value == "" {
			return thunderTarget{}, fmt.Errorf("thunder binding %s has no %q", from, key)
		}
	}

	secretName := cm.Data[keySecretName]
	if secretName == "" {
		return thunderTarget{}, fmt.Errorf("thunder binding %s has no %q", from, keySecretName)
	}
	// The binding names its own mirror namespace; this operator can only read
	// Secrets from its own (informer and RBAC both). Saying so beats a
	// cache-miss NotFound that looks like the Secret was never written.
	if ns := cm.Data[keySecretNamespace]; ns != "" && ns != r.PodNamespace {
		return thunderTarget{}, fmt.Errorf(
			"thunder binding %s mirrors its Secret into namespace %q, but this operator reads Secrets "+
				"only from its own namespace %q", from, ns, r.PodNamespace)
	}

	var sec corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: r.PodNamespace, Name: secretName}, &sec); err != nil {
		if apierrors.IsNotFound(err) {
			return thunderTarget{}, fmt.Errorf(
				"thunder binding %s names Secret %s/%s, which does not exist — "+
					"re-run deployments/scripts/setup-environment-thunder.sh %s %s to mirror it",
				from, r.PodNamespace, secretName, org, env)
		}
		return thunderTarget{}, fmt.Errorf("read thunder binding Secret %s/%s: %w", r.PodNamespace, secretName, err)
	}
	tgt.ClientID = string(sec.Data[keyClientID])
	tgt.ClientSecret = string(sec.Data[keyClientSecret])
	if tgt.ClientID == "" || tgt.ClientSecret == "" {
		return thunderTarget{}, fmt.Errorf(
			"thunder binding Secret %s/%s is missing %q or %q", r.PodNamespace, secretName, keyClientID, keyClientSecret)
	}
	return tgt, nil
}

// namespacedNames renders a ConfigMap list for an error message, sorted so the
// message is stable across list orderings.
func namespacedNames(items []corev1.ConfigMap) []string {
	out := make([]string, 0, len(items))
	for i := range items {
		out = append(out, items[i].Namespace+"/"+items[i].Name)
	}
	sort.Strings(out)
	return out
}

// clientCache holds one Thunder admin client per (org, env). The client is
// worth keeping across reconciles because it caches the `system` access token
// it mints; the cheap part (reading the binding) happens every pass, off the
// informer cache.
//
// Invalidation is by VALUE, not by event: the cached target is compared with
// the freshly resolved one, so any edit to the binding ConfigMap or its Secret
// — a rotated credential, a re-pointed adminURL — replaces the client on the
// next pass. The watches on those two objects only make that pass happen
// promptly; correctness does not depend on them firing.
type clientCache struct {
	mu      sync.Mutex
	clients map[string]cachedClient
}

type cachedClient struct {
	target thunderTarget
	client thunder.AdminClient
}

// get returns the admin client for tgt, building one through newClient when the
// cache holds nothing for that (org, env) or holds a client for a target that
// has since changed.
func (c *clientCache) get(tgt thunderTarget, newClient func(thunder.Config) thunder.AdminClient) thunder.AdminClient {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := bindingKeyOf(tgt.Org, tgt.Env)
	if cached, ok := c.clients[key]; ok && cached.target == tgt {
		return cached.client
	}
	cl := newClient(thunder.Config{
		BaseURL:                  tgt.AdminURL,
		ClientID:                 tgt.ClientID,
		ClientSecret:             tgt.ClientSecret,
		SystemResourceIdentifier: tgt.SystemResourceIdentifier,
	})
	if c.clients == nil {
		c.clients = map[string]cachedClient{}
	}
	c.clients[key] = cachedClient{target: tgt, client: cl}
	return cl
}

// clientFor resolves the Thunder client this CR's application belongs on.
func (r *Reconciler) clientFor(tgt thunderTarget) thunder.AdminClient {
	newClient := r.NewThunderClient
	if newClient == nil {
		newClient = thunder.New
	}
	return r.cache.get(tgt, newClient)
}
