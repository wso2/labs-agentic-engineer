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
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/wso2/aep/aectl/internal/bootstrap"
	"github.com/wso2/aep/aectl/internal/config"
	k8s "github.com/wso2/aep/aectl/internal/kubernetes"
	"github.com/wso2/aep/aectl/internal/openbao"
)

// A platform secret aectl GENERATES (as opposed to one it is given, like the
// Thunder admin credentials) is created once and never rewritten: something
// outside OpenBao holds it from then on. The Postgres volume keeps the role
// password it was initialised with, Thunder keeps each client's registered
// secret, GitHub keeps every hook's relay channel (derived from the relay
// seed), OpenSearch keeps its admin password. Rewriting one rotates it behind
// those holders' backs, and ESO carries the new value into the cluster only at
// its next refresh (up to an hour later), so the break shows up at the next pod
// restart, far from the run that caused it.
//
// The local OpenBao is in memory, so a pod restart does lose every key. The
// cluster still holds each one: ESO keeps the last value it synced in the
// target Secret. Seeding restores a missing key from there before it would
// generate a new one, which turns an OpenBao restart into a no-op for every
// holder above.

// generatedSecret is one platform secret aectl generates. generate is nil
// for a key an operator imports (`platform secret import`): seeding then only
// restores it from the cluster, and leaves it absent when nothing holds it.
type generatedSecret struct {
	path     string
	generate func() (string, error)
}

// importedPlatformSecrets are keys an operator imports that the platform reads
// through ESO, restored like the generated ones after an OpenBao restart: the
// Jev key and, with imported clients, the client secrets.
func importedPlatformSecrets() []generatedSecret {
	return append([]generatedSecret{{path: "aep/jev-api-key"}}, importedClientSecrets()...)
}

// importedClientNames are the clients whose secrets the IdP's admin creates
// where aectl creates none (on Asgardeo: deployments/scripts/ae-asgardeo-apps.sh)
// and imports under aep/idp-clients/<name>: aep-api's own client, the
// AE-only client aep-api calls every org's pod with, aectl's own client
// (OpenChoreo's Secret API), and the one org's publisher and AE Studio
// clients. They are the confidential clients of asgardeoClients, whose
// secrets aectl generates where it creates them.
var importedClientNames = []string{
	"aep-api-client",
	"ae-studio-internal",
	"aectl-openchoreo",
	"org-publisher",
	"org-studio",
}

// importedClientSecrets are the client secrets an install imports and never
// generates: those of importedClientNames with imported clients, none where
// aectl creates the clients (where it generates every client secret).
func importedClientSecrets() []generatedSecret {
	if platformIDPClients() != config.ClientsImported {
		return nil
	}
	out := make([]generatedSecret, 0, len(importedClientNames))
	for _, name := range importedClientNames {
		out = append(out, generatedSecret{path: "aep/idp-clients/" + name})
	}
	return out
}

// requireImportedClientSecrets is the imported-clients install's check that each
// imported client secret is in the vault. One the vault lost and the cluster
// still holds (the local OpenBao is in memory) is restored first, and
// returned. One that nothing holds is an error naming the command that
// imports it; it is never generated, since only the IdP's admin has it.
func requireImportedClientSecrets(st seedStore) ([]seededSecret, error) {
	secrets := importedClientSecrets()
	restored, err := seedGeneratedSecrets(secrets, st)
	if err != nil {
		return restored, err
	}
	var missing []string
	for _, s := range secrets {
		ok, err := st.exists(s.path)
		if err != nil {
			return restored, fmt.Errorf("check secret %s: %w", s.path, err)
		}
		if !ok {
			missing = append(missing, s.path)
		}
	}
	if len(missing) == 0 {
		return restored, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d client secret(s) of the platform IdP are not in OpenBao. With idp.clients imported aectl creates no client: the IdP's admin creates them (on Asgardeo with deployments/scripts/ae-asgardeo-apps.sh, which writes one file per secret). Import each:", len(missing))
	for _, path := range missing {
		fmt.Fprintf(&b, "\n  aectl platform secret import --path %s --value-file <file>", path)
	}
	return restored, errors.New(b.String())
}

// generatedThunderClientSecrets are the Thunder clients' generated secrets.
func generatedThunderClientSecrets() []generatedSecret {
	out := make([]generatedSecret, 0, len(generatedThunderClientNames))
	for _, name := range generatedThunderClientNames {
		out = append(out, generatedSecret{
			path:     "aep/idp-clients/" + name,
			generate: func() (string, error) { return thunderClientSecretValue(name) },
		})
	}
	return out
}

// webhookRelaySeedSecret is the per-org relay channels' seed.
var webhookRelaySeedSecret = generatedSecret{path: webhookRelaySeedPath, generate: webhookRelaySeed}

// generatedAsgardeoClientSecrets are the generated secrets of the
// confidential clients aectl creates in an Asgardeo organization. Each is 32
// random bytes as hex, the form ae-asgardeo-apps.sh creates applications
// with.
func generatedAsgardeoClientSecrets() []generatedSecret {
	var out []generatedSecret
	for _, d := range asgardeoClients() {
		if d.clientType != "confidential" {
			continue
		}
		out = append(out, generatedSecret{
			path:     d.vaultPath(),
			generate: func() (string, error) { return bootstrap.GenerateHex(32) },
		})
	}
	return out
}

// generatedClientSecrets are the client secrets this install generates: the
// secrets of the clients aectl creates (on a ThunderID it administers, or in
// an Asgardeo organization), none where they are imported.
func generatedClientSecrets() []generatedSecret {
	switch platformIDPClients() {
	case config.ClientsThunder:
		return generatedThunderClientSecrets()
	case config.ClientsAsgardeo:
		return generatedAsgardeoClientSecrets()
	}
	return nil
}

// generatedPlatformSecrets is every secret an install generates.
func generatedPlatformSecrets() []generatedSecret {
	return append([]generatedSecret{
		{path: "aep/postgres-password", generate: func() (string, error) { return bootstrap.GeneratePassword(32) }},
		{path: "aep/opensearch-password", generate: func() (string, error) { return bootstrap.GeneratePassword(24) }},
		webhookRelaySeedSecret,
	}, generatedClientSecrets()...)
}

// seededSecret is a key seedGeneratedSecrets wrote. value is held only to
// verify the cluster caught up; it is never logged. from names the cluster
// Secret it was restored from ("" = generated).
type seededSecret struct{ path, value, from string }

// errSecretExists is what a put returns when the store refused to overwrite an
// existing key (KV v2 check-and-set), which seeding treats as "already there".
var errSecretExists = errors.New("secret already exists")

// recoverFunc returns the value ESO last synced from an OpenBao path into the
// cluster and the Secret it read it from, or "" when no Secret holds one.
type recoverFunc func(path string) (value, from string, err error)

// seedStore is the store side of seeding: exists reports a key, put writes one
// create-only (errSecretExists when it is already there), recover (optional)
// reads a key's last synced value back out of the cluster.
type seedStore struct {
	exists  func(path string) (bool, error)
	put     func(path, value string) error
	recover recoverFunc
}

// seedGeneratedSecrets writes each of secrets that the store lacks and never
// touches one it has. A missing key takes the value the cluster still holds
// for it when there is one, a fresh value only when there is none. A key that
// appears between the check and the write (errSecretExists) is kept. It
// returns what it wrote.
func seedGeneratedSecrets(secrets []generatedSecret, st seedStore) ([]seededSecret, error) {
	var seeded []seededSecret
	for _, g := range secrets {
		ok, err := st.exists(g.path)
		if err != nil {
			return seeded, fmt.Errorf("check secret %s: %w", g.path, err)
		}
		if ok {
			continue
		}
		var value, from string
		if st.recover != nil {
			// Fails closed: a failed read must not be taken for "the cluster
			// holds nothing", which would rotate the key.
			if value, from, err = st.recover(g.path); err != nil {
				return seeded, fmt.Errorf("read the cluster's copy of %s: %w", g.path, err)
			}
		}
		if value == "" && g.generate == nil {
			continue
		}
		if value == "" {
			if value, err = g.generate(); err != nil {
				return seeded, fmt.Errorf("generate %s: %w", g.path, err)
			}
			from = ""
		}
		if err := st.put(g.path, value); err != nil {
			if errors.Is(err, errSecretExists) {
				continue
			}
			return seeded, fmt.Errorf("write %s: %w", g.path, err)
		}
		seeded = append(seeded, seededSecret{path: g.path, value: value, from: from})
	}
	return seeded, nil
}

// seedStore is s as a seed target; recover may be nil.
func (s *openBaoSession) seedStore(ctx context.Context, recover recoverFunc) seedStore {
	return seedStore{
		exists:  func(path string) (bool, error) { return s.pathExists(ctx, path) },
		put:     func(path, value string) error { return s.putCreateOnly(ctx, path, value) },
		recover: recover,
	}
}

// platformInput is a secret an install is given, written as given.
type platformInput struct{ path, value string }

// writePlatformSecrets writes an install's inputs (overwriting: they are what
// the caller supplied this run) and seeds the generated secrets the store
// lacks. It returns what it seeded.
func writePlatformSecrets(ctx context.Context, s *openBaoSession, inputs []platformInput, recover recoverFunc) ([]seededSecret, error) {
	for _, in := range inputs {
		if _, err := openbao.Must(ctx, "PUT", s.baseURL, s.token, "/v1/secret/data/"+in.path, map[string]interface{}{
			"data": map[string]interface{}{"value": in.value},
		}); err != nil {
			return nil, fmt.Errorf("write %s: %w", in.path, err)
		}
	}
	return seedGeneratedSecrets(append(generatedPlatformSecrets(), importedPlatformSecrets()...), s.seedStore(ctx, recover))
}

// externalSecretAPIVersion is the ESO API the chart's ExternalSecrets use.
const externalSecretAPIVersion = "external-secrets.io/v1"

// externalSecretLister lists and patches ExternalSecrets (k8s.Applier).
type externalSecretLister interface {
	List(ctx context.Context, apiVersion, kind, namespace string) ([]unstructured.Unstructured, error)
	Get(ctx context.Context, apiVersion, kind, namespace, name string) (*unstructured.Unstructured, error)
	Patch(ctx context.Context, apiVersion, kind, namespace, name string, patchType types.PatchType, body []byte) error
}

// externalSecretRef is one key of an ExternalSecret: the Secret key it fills
// from an OpenBao path's "value" property.
type externalSecretRef struct {
	namespace, externalSecret, secret, key, path string
}

// externalSecretRefs flattens ExternalSecrets into their per-key refs. Keys
// reading another property than "value" are left out: aectl writes "value".
func externalSecretRefs(items []unstructured.Unstructured) []externalSecretRef {
	var refs []externalSecretRef
	for _, es := range items {
		target, _, _ := unstructured.NestedString(es.Object, "spec", "target", "name")
		if target == "" {
			target = es.GetName()
		}
		data, _, _ := unstructured.NestedSlice(es.Object, "spec", "data")
		for _, d := range data {
			m, ok := d.(map[string]interface{})
			if !ok {
				continue
			}
			key, _, _ := unstructured.NestedString(m, "secretKey")
			path, _, _ := unstructured.NestedString(m, "remoteRef", "key")
			prop, _, _ := unstructured.NestedString(m, "remoteRef", "property")
			if key == "" || path == "" || (prop != "" && prop != "value") {
				continue
			}
			refs = append(refs, externalSecretRef{
				namespace: es.GetNamespace(), externalSecret: es.GetName(),
				secret: target, key: key, path: path,
			})
		}
	}
	return refs
}

// clusterSecretRecovery reads a path's last synced value from the Secrets the
// cluster's ExternalSecrets fill from it, the platform namespace's first (the
// one the platform's own pods and Thunder registration read), then the rest
// in a stable order. The first non-empty value wins.
func clusterSecretRecovery(ctx context.Context, es externalSecretLister, c kubernetes.Interface, platformNamespace string) (recoverFunc, error) {
	items, err := es.List(ctx, externalSecretAPIVersion, "ExternalSecret", "")
	if err != nil {
		return nil, fmt.Errorf("list ExternalSecrets: %w", err)
	}
	byPath := map[string][]externalSecretRef{}
	for _, r := range externalSecretRefs(items) {
		byPath[r.path] = append(byPath[r.path], r)
	}
	for _, refs := range byPath {
		sort.SliceStable(refs, func(i, j int) bool {
			pi, pj := refs[i].namespace == platformNamespace, refs[j].namespace == platformNamespace
			if pi != pj {
				return pi
			}
			return refs[i].namespace+"/"+refs[i].secret < refs[j].namespace+"/"+refs[j].secret
		})
	}
	return func(path string) (string, string, error) {
		for _, r := range byPath[path] {
			sec, err := c.CoreV1().Secrets(r.namespace).Get(ctx, r.secret, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				continue
			}
			if err != nil {
				return "", "", fmt.Errorf("read Secret %s/%s: %w", r.namespace, r.secret, err)
			}
			if v := string(sec.Data[r.key]); v != "" {
				return v, r.namespace + "/" + r.secret, nil
			}
		}
		return "", "", nil
	}, nil
}

// newClusterSecretRecovery is clusterSecretRecovery on the kubeconfig's cluster.
func newClusterSecretRecovery(ctx context.Context, platformNamespace string) (recoverFunc, error) {
	a, err := k8s.NewApplier(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("connect to cluster: %w", err)
	}
	c, err := k8s.NewClient(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("connect to cluster: %w", err)
	}
	return clusterSecretRecovery(ctx, a, c, platformNamespace)
}

// platformSecretPaths is every OpenBao path the platform's pods read through
// ESO that an install writes or requires: its inputs, the generated secrets,
// and the imported client secrets. Their Secrets are synced
// before anything reads them.
func platformSecretPaths() []string {
	paths := append([]string(nil), platformInputPaths()...)
	for _, g := range generatedPlatformSecrets() {
		paths = append(paths, g.path)
	}
	for _, g := range importedClientSecrets() {
		paths = append(paths, g.path)
	}
	return paths
}

// platformInputPaths are the paths an install writes from its inputs. The
// Thunder admin credentials are inputs only where the IdP is Thunder.
func platformInputPaths() []string {
	paths := []string{
		"aep/anthropic-api-key",
		"aep/opensearch-username",
	}
	if !administersThunder() {
		return paths
	}
	return append(paths,
		"aep/thunder-admin/client-id",
		"aep/thunder-admin/client-secret",
	)
}

// externalSecretSyncTimeout bounds the wait for ESO to re-read OpenBao.
const externalSecretSyncTimeout = 90 * time.Second

// syncExternalSecretsNow makes ESO re-read OpenBao now for every ExternalSecret
// in namespace that fills a key from one of paths, and waits until each has
// synced since. It returns the target Secrets that did not (Secret name ->
// reason).
//
// A reader of an ESO-synced Secret (the Postgres role sync, Thunder client
// registration) must run after this: the Secret otherwise holds whatever ESO
// synced up to an hour ago, and acting on a value ESO is about to replace is
// a rotation that lands at the next refresh.
func syncExternalSecretsNow(ctx context.Context, es externalSecretLister, namespace string, paths []string, timeout time.Duration) (map[string]string, error) {
	items, err := es.List(ctx, externalSecretAPIVersion, "ExternalSecret", namespace)
	if err != nil {
		return nil, fmt.Errorf("list ExternalSecrets in %s: %w", namespace, err)
	}
	want := map[string]bool{}
	for _, p := range paths {
		want[p] = true
	}
	targets := map[string]string{} // ExternalSecret -> target Secret
	for _, r := range externalSecretRefs(items) {
		if want[r.path] {
			targets[r.externalSecret] = r.secret
		}
	}

	// Second resolution: a sync in the same second as the nudge counts.
	since := time.Now().UTC().Truncate(time.Second)
	patch := fmt.Sprintf(`{"metadata":{"annotations":{"force-sync":%q}}}`, strconv.FormatInt(time.Now().UnixNano(), 10))
	unsynced := map[string]string{}
	for name, secret := range targets {
		if err := es.Patch(ctx, externalSecretAPIVersion, "ExternalSecret", namespace, name, types.MergePatchType, []byte(patch)); err != nil {
			unsynced[secret] = fmt.Sprintf("could not nudge ExternalSecret %s: %v", name, err)
			delete(targets, name)
		}
	}

	deadline := time.Now().Add(timeout)
	for len(targets) > 0 {
		for name := range targets {
			obj, err := es.Get(ctx, externalSecretAPIVersion, "ExternalSecret", namespace, name)
			if err == nil && obj != nil && syncedSince(obj, since) {
				delete(targets, name)
			}
		}
		if len(targets) == 0 || time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	for name, secret := range targets {
		unsynced[secret] = fmt.Sprintf("ExternalSecret %s/%s did not sync from OpenBao within %s", namespace, name, timeout)
	}
	return unsynced, nil
}

// syncedSince reports an ExternalSecret Ready with a refresh at or after t.
func syncedSince(es *unstructured.Unstructured, t time.Time) bool {
	refreshed, _, _ := unstructured.NestedString(es.Object, "status", "refreshTime")
	at, err := time.Parse(time.RFC3339, refreshed)
	if err != nil || at.Before(t) {
		return false
	}
	conds, _, _ := unstructured.NestedSlice(es.Object, "status", "conditions")
	for _, c := range conds {
		m, ok := c.(map[string]interface{})
		if ok && m["type"] == "Ready" {
			return m["status"] == "True"
		}
	}
	return false
}

// syncPlatformExternalSecrets is syncExternalSecretsNow of every platform
// secret path on the kubeconfig's cluster.
func syncPlatformExternalSecrets(ctx context.Context, namespace string) (map[string]string, error) {
	a, err := k8s.NewApplier(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("connect to cluster: %w", err)
	}
	return syncExternalSecretsNow(ctx, a, namespace, platformSecretPaths(), externalSecretSyncTimeout)
}

// clientsReadingUnsynced maps unsynced Secrets to the clients of idpClients
// that read them (clientID -> reason): registering one would hand the IdP
// the stale value.
func clientsReadingUnsynced(unsynced map[string]string) map[string]string {
	skip := map[string]string{}
	for _, d := range idpClients() {
		if reason, ok := unsynced[d.secretNameOrDefault()]; ok && d.clientType == "confidential" {
			skip[d.clientID] = reason
		}
	}
	return skip
}

// orgClientVaultPaths are the vault paths of the org's two client secrets
// (orgClients).
var orgClientVaultPaths = []string{"aep/idp-clients/org-publisher", "aep/idp-clients/org-studio"}

// regeneratedOrgClientSecrets lists the org's client secrets that a seed run
// generated, and did not restore from a cluster Secret. aep-api stores these
// two in the org's references when the org connects GitHub, so a new value
// leaves an org that is already connected with the previous one.
func regeneratedOrgClientSecrets(seeded []seededSecret) []string {
	var paths []string
	for _, s := range seeded {
		if s.from == "" && slices.Contains(orgClientVaultPaths, s.path) {
			paths = append(paths, s.path)
		}
	}
	return paths
}

// orgClientRepairWarning is the warning for the org client secrets that a
// seed run generated, or "" when it generated none. The steps are in the
// order that works: a restart of aep-api or a GitHub reconnect before aectl
// has registered the new secret stores, in the org, a secret that the IdP
// does not accept. aectl cannot do the repair: only aep-api writes an org's
// references, on GitHub connect.
func orgClientRepairWarning(seeded []seededSecret) string {
	paths := regeneratedOrgClientSecrets(seeded)
	if len(paths) == 0 {
		return ""
	}
	return fmt.Sprintf("Generated a new secret for the org's client (%s). An org that already connected GitHub still holds the previous secret, so its builds and its AE Studio cannot get a token. To repair, in this order: 1. make sure that aectl registered the org's clients (if this command stopped or skipped them, run 'aectl platform sync-clients' until it registers them); 2. restart aep-api; 3. reconnect GitHub in the console.", strings.Join(paths, ", "))
}

// describeSeeded is the one-line, value-free account of a seed run.
func describeSeeded(seeded []seededSecret) []string {
	var lines []string
	for _, s := range seeded {
		if s.from != "" {
			lines = append(lines, fmt.Sprintf("Restored %s from Secret %s", s.path, s.from))
		} else {
			lines = append(lines, "Generated "+s.path)
		}
	}
	return lines
}
