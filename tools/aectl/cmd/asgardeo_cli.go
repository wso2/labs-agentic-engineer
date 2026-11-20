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

// asgardeo_cli.go — AE's clients in an Asgardeo organization, created by
// aectl (idp.clients asgardeo).
//
// The organization's admin gives an admin credential for one command, in two
// files. aectl holds it in memory for that command: it is not stored in the
// cluster or the config, and is never printed.
//
// The direction of a client secret:
//   - an application aectl creates takes the secret aectl generated and
//     stored in the vault, as on ThunderID;
//   - an application that exists cannot be given a secret and cannot show the
//     one it has. aectl asks the organization for a token with the vault's
//     secret. When that is refused, and only with --replace-client-secrets,
//     Asgardeo issues a new secret and aectl writes it over the vault path.
//
// A replaced secret reaches a pod only when the pod starts. So aectl records,
// on each Deployment that reads a client Secret, a checksum of the Secret as
// its pods started with it, and every run restarts a Deployment whose
// checksum is no longer the Secret's. A run that failed between the write to
// the vault and the restart is so completed by the next run.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/wso2/aep/aectl/internal/asgardeo"
	"github.com/wso2/aep/aectl/internal/config"
	"github.com/wso2/aep/aectl/internal/ui"
)

var (
	idpAdminClientIDFile     string
	idpAdminClientSecretFile string
	replaceClientSecrets     bool
)

// registerIDPAdminFlags adds the flags of a command that creates clients in
// an Asgardeo organization. They are not bound to config keys: the credential
// belongs to one run and is never stored.
func registerIDPAdminFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&idpAdminClientIDFile, "idp-admin-client-id-file", "", "File holding the client id of the Asgardeo organization's admin application (idp.clients asgardeo only). Read for this run; never stored")
	f.StringVar(&idpAdminClientSecretFile, "idp-admin-client-secret-file", "", "File holding the client secret of the Asgardeo organization's admin application (idp.clients asgardeo only). Read for this run; never stored or printed")
	f.BoolVar(&replaceClientSecrets, "replace-client-secrets", false, "idp.clients asgardeo only: where an application exists and the secret in this install's vault is not its secret, have Asgardeo issue a new one. The old secret stops working at once, so do not use it while another install uses the same organization")
}

// asgardeoPlatformClientIDs are the platform's clients an Asgardeo
// organization needs, of aepThunderClients. The rest of that list serves what
// installs only beside a ThunderID aectl administers.
var asgardeoPlatformClientIDs = []string{"aep-console-client", "aep-api-client", "ae-studio-internal-client"}

// asgardeoClients is the five clients aectl creates in an Asgardeo
// organization: the console's, aep-api's own, the AE-only client, and the
// org's two. Each is the definition the ThunderID list holds, so a client
// reads the same K8s Secret and vault path on both IdPs.
func asgardeoClients() []idpClientDef {
	var out []idpClientDef
	for _, d := range aepThunderClients {
		if slices.Contains(asgardeoPlatformClientIDs, d.clientID) {
			out = append(out, d)
		}
	}
	return append(out, orgClients()...)
}

// readIDPAdminCredential reads the admin application's client id and secret
// from the two files the flags name. Each value is trimmed, and an empty one
// is refused.
func readIDPAdminCredential() (clientID, clientSecret string, err error) {
	if idpAdminClientIDFile == "" || idpAdminClientSecretFile == "" {
		return "", "", errors.New("idp.clients is asgardeo: aectl creates the clients with the credential of the organization's admin application. Pass --idp-admin-client-id-file and --idp-admin-client-secret-file")
	}
	read := func(flag, file string) (string, error) {
		raw, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("--%s: %w", flag, err)
		}
		v := strings.TrimSpace(string(raw))
		if v == "" {
			return "", fmt.Errorf("--%s: %s is empty", flag, file)
		}
		return v, nil
	}
	if clientID, err = read("idp-admin-client-id-file", idpAdminClientIDFile); err != nil {
		return "", "", err
	}
	if clientSecret, err = read("idp-admin-client-secret-file", idpAdminClientSecretFile); err != nil {
		return "", "", err
	}
	return clientID, clientSecret, nil
}

// refuseIDPAdminFlags is the error for an admin flag given where aectl
// creates no client in Asgardeo, or nil. A flag that is read by nothing would
// look as if it took effect.
func refuseIDPAdminFlags() error {
	if platformIDPClients() == config.ClientsAsgardeo {
		return nil
	}
	if idpAdminClientIDFile != "" || idpAdminClientSecretFile != "" || replaceClientSecrets {
		return fmt.Errorf("--idp-admin-client-id-file, --idp-admin-client-secret-file and --replace-client-secrets are read only when idp.clients is %s (it is %s)", config.ClientsAsgardeo, platformIDPClients())
	}
	return nil
}

// preflightAsgardeo is the check a command makes before it changes anything:
// the credential files are readable, the organization issues a management
// token with the scopes this run needs, and each client is looked up. It
// prints which applications exist and returns the organization's admin
// client, which holds the management token for the rest of the command. The
// credential itself is not kept.
func preflightAsgardeo(ctx context.Context, tokenURL string) (asgardeoAdmin, error) {
	clientID, clientSecret, err := readIDPAdminCredential()
	if err != nil {
		return nil, err
	}
	admin, err := asgardeo.New(ctx, asgardeo.Config{
		APIURL:        viper.GetString("asgardeo.api_url"),
		TokenURL:      tokenURL,
		AdminClientID: clientID, AdminClientSecret: clientSecret,
	})
	if err != nil {
		return nil, err
	}
	if replaceClientSecrets && !admin.CanRegenerateSecrets() {
		return nil, fmt.Errorf("--replace-client-secrets needs the scope %s: authorize the admin application for the Application Management API (its API Authorization tab) with that scope", asgardeo.ScopeRegenerateSecret)
	}
	ui.Success("Asgardeo organization: management token issued (" + viper.GetString("asgardeo.api_url") + ")")
	for _, def := range asgardeoClients() {
		id, err := admin.Find(ctx, def.clientID)
		if err != nil {
			return nil, err
		}
		if id == "" {
			ui.Detail(def.clientID + ": will be created")
		} else {
			ui.Detail(def.clientID + ": exists")
		}
	}
	return admin, nil
}

// asgardeoAdmin is what ensureAsgardeoClients needs of *asgardeo.Client.
type asgardeoAdmin interface {
	Find(ctx context.Context, clientID string) (string, error)
	CreateMachine(ctx context.Context, clientID, secret string) (string, error)
	CreateBrowser(ctx context.Context, clientID, consoleURL string) (string, error)
	Problems(ctx context.Context, app asgardeo.App, appID string) ([]string, error)
	SecretWorks(ctx context.Context, clientID, secret string) (bool, error)
	RegenerateSecret(ctx context.Context, appID string) (string, error)
}

// vaultPut writes a value over a vault path.
type vaultPut func(ctx context.Context, vaultPath, value string) error

// asgardeoApp is def as the organization holds it.
func asgardeoApp(def idpClientDef, consoleURL string) asgardeo.App {
	if def.clientType == "confidential" {
		return asgardeo.App{ClientID: def.clientID, Kind: asgardeo.Machine}
	}
	return asgardeo.App{ClientID: def.clientID, Kind: asgardeo.Browser, ConsoleURL: consoleURL}
}

// ensureAsgardeoClients brings each client of defs to what AE needs, and
// reports each with report(clientID, state):
//   - one the organization lacks is created, a confidential one with its
//     secret from secretsByName (K8s Secret name -> key -> value);
//   - one that exists is not changed. Its settings are read back, and each
//     wrong one is a failure;
//   - a confidential one that exists must accept its secret. If it does not,
//     and replace is set, Asgardeo issues a new secret, which store writes
//     over the client's vault path. Without replace it is a failure.
//
// Every client is tried. The failures come back as one error that names each
// client and what to do. replaced is the clients whose secret was replaced,
// also when the run ends in an error.
func ensureAsgardeoClients(
	ctx context.Context, admin asgardeoAdmin, defs []idpClientDef,
	secretsByName map[string]map[string]string, consoleURL string, replace bool,
	store vaultPut, report func(clientID, state string),
) (replaced []idpClientDef, err error) {
	var failures []string
	fail := func(def idpClientDef, problems ...string) {
		for _, p := range problems {
			failures = append(failures, def.clientID+": "+p)
		}
	}
	for _, def := range defs {
		app := asgardeoApp(def, consoleURL)
		machine := app.Kind == asgardeo.Machine
		var secret string
		if machine {
			if secret = secretsByName[def.secretNameOrDefault()][def.secretKey]; secret == "" {
				return replaced, fmt.Errorf("secret key %q missing from Secret %s", def.secretKey, def.secretNameOrDefault())
			}
		}
		id, err := admin.Find(ctx, def.clientID)
		if err != nil {
			return replaced, err
		}
		state := "exists"
		if id == "" {
			state = "created"
			if machine {
				id, err = admin.CreateMachine(ctx, def.clientID, secret)
			} else {
				id, err = admin.CreateBrowser(ctx, def.clientID, consoleURL)
			}
			if err != nil {
				return replaced, err
			}
		}
		problems, err := admin.Problems(ctx, app, id)
		if err != nil {
			return replaced, err
		}
		if len(problems) > 0 {
			fail(def, problems...)
			continue
		}
		if machine && state == "exists" {
			works, err := admin.SecretWorks(ctx, def.clientID, secret)
			if err != nil {
				return replaced, err
			}
			if !works && !replace {
				fail(def, fmt.Sprintf("the secret in this install's vault is not the application's secret. Run the command again with --replace-client-secrets: Asgardeo then issues a new secret, and the old one stops working at once. If another install uses this organization, import the secret that install holds instead (aectl platform secret import --path %s)", def.vaultPath()))
				continue
			}
			if !works {
				fresh, err := admin.RegenerateSecret(ctx, id)
				if err != nil {
					return replaced, fmt.Errorf("%s: %w", def.clientID, err)
				}
				// Asgardeo holds the new secret from here on, and nothing
				// else does: a failed write leaves the client without one.
				if err := store(ctx, def.vaultPath(), fresh); err != nil {
					return replaced, fmt.Errorf("%s: Asgardeo issued a new secret, but it could not be written to the vault (%w). Run the command again with --replace-client-secrets", def.clientID, err)
				}
				replaced = append(replaced, def)
				state = "exists, secret replaced"
			}
		}
		report(def.clientID, state)
	}
	if len(failures) > 0 {
		return replaced, fmt.Errorf("%d problem(s) with the platform IdP's clients:\n  - %s\nCorrect a wrong setting in the Asgardeo console, or delete the application there and run this command again", len(failures), strings.Join(failures, "\n  - "))
	}
	return replaced, nil
}

// doAsgardeoSetup creates and checks AE's clients in the Asgardeo
// organization, at the place doThunderSetup has on ThunderID: after the
// chart's Secrets have synced, before the wait for pods. admin is the client
// preflightAsgardeo returned, and store writes a replaced secret to the
// vault. skipClients (clientID -> reason) are left untouched with a warning.
// It ends by bringing the Deployments in line with the client Secrets
// (reconcileClientSecretReaders), whether or not the clients were set up.
func doAsgardeoSetup(ctx context.Context, k8sClient *kubernetes.Clientset, platformNamespace, consoleURL string, admin asgardeoAdmin, store vaultPut, skipClients map[string]string) error {
	secretsByName, err := waitForClientSecrets(ctx, k8sClient, platformNamespace, skipClients)
	if err != nil {
		return err
	}
	for id, reason := range skipClients {
		ui.Warn(fmt.Sprintf("Skipped %s: %s", id, reason))
	}
	// The Secrets that were read: those of the clients this run sets up.
	secretNames := slices.Sorted(maps.Keys(secretsByName))
	// Before anything changes, each Deployment gets the checksum of the
	// Secret as it is now, if it has none: its pods started with that value.
	if err := recordClientSecretChecksums(ctx, k8sClient, platformNamespace, secretNames); err != nil {
		return err
	}
	replaced, err := ensureAsgardeoClients(ctx, admin, clientsToRegister(asgardeoClients(), secretsByName, skipClients),
		secretsByName, consoleURL, replaceClientSecrets, store,
		func(clientID, state string) { ui.Success(fmt.Sprintf("%s (%s)", clientID, state)) })
	// A replaced secret is in the vault whether or not the run failed later,
	// and an earlier run may have left a Deployment behind: the cluster is
	// brought in line either way.
	reconcileErr := reconcileClientSecretReaders(ctx, k8sClient, platformNamespace, secretNames, replaced,
		func(ctx context.Context) (map[string]string, error) {
			return syncPlatformExternalSecrets(ctx, platformNamespace)
		})
	if err == nil {
		err = reconcileErr
	}
	return err
}

// lazyVaultPut is a vaultPut that connects to OpenBao at its first write, for
// a command that has no session open, and the function that closes the
// connection.
func lazyVaultPut() (put vaultPut, stop func()) {
	var session *openBaoSession
	put = func(ctx context.Context, vaultPath, value string) error {
		if session == nil {
			s, err := openOpenBaoSession(ctx)
			if err != nil {
				return fmt.Errorf("connect to OpenBao: %w", err)
			}
			session = s
		}
		return session.put(ctx, vaultPath, value)
	}
	stop = func() {
		if session != nil {
			session.stop()
		}
	}
	return put, stop
}

// replacedOrgClientWarning is the warning for the org's clients among
// replaced, or "" when none of them is the org's. aep-api stores the secrets
// of the org's two clients in the org's references when the org connects
// GitHub, so an org that is already connected still holds the old one. One
// step is left to the operator, and only aep-api can do the rest: a GitHub
// reconnect, after this command has restarted aep-api with the new secret.
// It is not orgClientRepairWarning: that one is for a secret aectl generated
// before the client was registered, where the restart is the operator's too.
func replacedOrgClientWarning(replaced []idpClientDef) string {
	var ids []string
	for _, def := range replaced {
		if slices.Contains(orgClientVaultPaths, def.vaultPath()) {
			ids = append(ids, def.clientID)
		}
	}
	if len(ids) == 0 {
		return ""
	}
	return fmt.Sprintf("Asgardeo issued a new secret for the org's client (%s). An org that already connected GitHub still holds the previous secret, so its builds and its AE Studio cannot get a token. When this command has completed without an error, reconnect GitHub in the console: aep-api then stores the new secret for the org.", strings.Join(ids, ", "))
}

// clientSecretChecksumAnnotation is the annotation, on a Deployment, that
// holds the checksum of the client Secret secretName as the Deployment's pods
// started with it.
func clientSecretChecksumAnnotation(secretName string) string {
	return "aep.wso2.com/client-secret-checksum." + secretName
}

// secretChecksum is a short fingerprint of a Secret's data: enough to see
// that a value changed, and nothing a value can be computed from.
func secretChecksum(data map[string][]byte) string {
	h := sha256.New()
	for _, k := range slices.Sorted(maps.Keys(data)) {
		_, _ = fmt.Fprintf(h, "%d:%s=%d:", len(k), k, len(data[k]))
		_, _ = h.Write(data[k])
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// readersOfSecret is the Deployments in namespace whose pods take env from
// the Secret name, sorted by name.
func readersOfSecret(ctx context.Context, c kubernetes.Interface, namespace, name string) ([]appsv1.Deployment, error) {
	deps, err := c.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list Deployments in %s: %w", namespace, err)
	}
	var out []appsv1.Deployment
	for _, d := range deps.Items {
		spec := d.Spec.Template.Spec
		if containersReadSecret(append(append([]corev1.Container{}, spec.InitContainers...), spec.Containers...), name) {
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b appsv1.Deployment) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// patchDeployment merge-patches a Deployment with patch.
func patchDeployment(ctx context.Context, c kubernetes.Interface, namespace, name string, patch map[string]any) error {
	raw, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	if _, err := c.AppsV1().Deployments(namespace).Patch(ctx, name, types.StrategicMergePatchType, raw, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("patch Deployment %s/%s: %w", namespace, name, err)
	}
	return nil
}

// recordClientSecretChecksums writes, on each Deployment that reads one of
// the Secrets secretNames and has no checksum for it, the checksum of the
// Secret as it is now. It changes no pod. It runs before a run changes a
// secret, so a Deployment aectl sees for the first time is recorded with the
// value its pods started with.
func recordClientSecretChecksums(ctx context.Context, c kubernetes.Interface, namespace string, secretNames []string) error {
	for _, name := range secretNames {
		sec, err := c.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("read Secret %s/%s: %w", namespace, name, err)
		}
		readers, err := readersOfSecret(ctx, c, namespace, name)
		if err != nil {
			return err
		}
		key := clientSecretChecksumAnnotation(name)
		for _, d := range readers {
			if d.Annotations[key] != "" {
				continue
			}
			if err := patchDeployment(ctx, c, namespace, d.Name, map[string]any{
				"metadata": map[string]any{"annotations": map[string]string{key: secretChecksum(sec.Data)}},
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// restartStaleSecretReaders restarts each Deployment whose recorded checksum
// of one of the Secrets secretNames is not the Secret's checksum now, and
// records the new one. The restart and the record are one patch, so a
// Deployment is never recorded as current without its restart. It returns
// the Deployments it restarted.
func restartStaleSecretReaders(ctx context.Context, c kubernetes.Interface, namespace string, secretNames []string) ([]string, error) {
	var restarted []string
	for _, name := range secretNames {
		sec, err := c.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return restarted, fmt.Errorf("read Secret %s/%s: %w", namespace, name, err)
		}
		readers, err := readersOfSecret(ctx, c, namespace, name)
		if err != nil {
			return restarted, err
		}
		key, sum := clientSecretChecksumAnnotation(name), secretChecksum(sec.Data)
		for _, d := range readers {
			if d.Annotations[key] == sum {
				continue
			}
			if err := patchDeployment(ctx, c, namespace, d.Name, map[string]any{
				"metadata": map[string]any{"annotations": map[string]string{key: sum}},
				"spec": map[string]any{"template": map[string]any{"metadata": map[string]any{
					"annotations": map[string]string{"kubectl.kubernetes.io/restartedAt": time.Now().Format(time.RFC3339)},
				}}},
			}); err != nil {
				return restarted, err
			}
			if !slices.Contains(restarted, d.Name) {
				restarted = append(restarted, d.Name)
			}
		}
	}
	return restarted, nil
}

// reconcileClientSecretReaders brings the Deployments in line with the
// client Secrets secretNames at the end of a run.
//
// Where Asgardeo issued new secrets this run (replaced), sync first makes ESO
// re-read the vault; it returns the Secrets that did not catch up (Secret
// name -> reason). Then every Deployment whose recorded checksum is not its
// Secret's is restarted, since a running pod keeps the value it started
// with. That covers this run's secrets and a restart an earlier run did not
// reach.
//
// A replaced secret whose Secret did not sync is an error: Asgardeo already
// refuses the old value, and the pods still hold it. The next run completes
// it: the Secret syncs, its checksum changes, and the Deployments restart.
//
// A replaced secret of one of the org's clients ends with
// replacedOrgClientWarning.
func reconcileClientSecretReaders(ctx context.Context, c kubernetes.Interface, platformNamespace string, secretNames []string, replaced []idpClientDef, sync func(ctx context.Context) (map[string]string, error)) error {
	var unsynced map[string]string
	if len(replaced) > 0 {
		var err error
		if unsynced, err = sync(ctx); err != nil {
			return fmt.Errorf("sync platform Secrets after replacing client secrets: %w", err)
		}
	}
	restarted, err := restartStaleSecretReaders(ctx, c, platformNamespace, secretNames)
	if len(restarted) > 0 {
		ui.Detail("Restarted " + strings.Join(restarted, ", ") + ": a client secret they read has changed")
	}
	if err != nil {
		return fmt.Errorf("%w. Run '%s' again: it restarts what still holds an old client secret", err, syncClientsCommand())
	}

	var pending []string
	for _, def := range replaced {
		if reason, ok := unsynced[def.secretNameOrDefault()]; ok {
			pending = append(pending, fmt.Sprintf("%s (Secret %s: %s)", def.clientID, def.secretNameOrDefault(), reason))
		}
	}
	if msg := replacedOrgClientWarning(replaced); msg != "" {
		ui.Warn(msg)
	}
	if len(pending) > 0 {
		return fmt.Errorf("the cluster Secret did not sync after Asgardeo issued a new secret for %d client(s): %s. The pods that read it still hold the old secret, which Asgardeo refuses. Run '%s' again: it syncs the Secret and restarts what reads it", len(pending), strings.Join(pending, "; "), syncClientsCommand())
	}
	return nil
}
