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
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/viper"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/wso2/aep/aectl/internal/asgardeo"
)

// setAsgardeoClientsConfig is a config where aectl creates the clients in an
// Asgardeo organization. setAsgardeoConfig is the same organization with
// imported clients.
func setAsgardeoClientsConfig(t *testing.T) {
	t.Helper()
	setAsgardeoConfig(t)
	viper.Set("idp.clients", "asgardeo")
	viper.Set("asgardeo.api_url", "https://api.asgardeo.io/t/acmecorp/api/server/v1")
}

func clientIDsOf(defs []idpClientDef) []string {
	var ids []string
	for _, d := range defs {
		ids = append(ids, d.clientID)
	}
	return ids
}

// aectl creates five clients in an Asgardeo organization: the ones the
// organization's admin creates with imported clients. Each is the definition
// the ThunderID list holds, so it reads the same Secret and vault path.
func TestAsgardeoClients(t *testing.T) {
	setAsgardeoClientsConfig(t)
	got := asgardeoClients()
	want := []string{"aep-api-client", "ae-studio-internal-client", "aep-console-client", "aep-publisher-default", "ae-studio-default"}
	if ids := clientIDsOf(got); !slices.Equal(ids, want) {
		t.Fatalf("clients = %v, want %v", ids, want)
	}
	var vaultNames []string
	for _, d := range got {
		if i := slices.IndexFunc(thunderClients(), func(t idpClientDef) bool { return t.clientID == d.clientID }); i < 0 {
			t.Errorf("%s is not a client of the ThunderID list", d.clientID)
		}
		if d.clientType == "confidential" {
			vaultNames = append(vaultNames, d.vaultName)
		}
	}
	// The confidential ones are exactly the clients whose secrets an install
	// with imported clients imports.
	if !slices.Equal(vaultNames, importedClientNames) {
		t.Errorf("vault names %v, want importedClientNames %v", vaultNames, importedClientNames)
	}
	if ids := clientIDsOf(idpClients()); !slices.Equal(ids, want) {
		t.Errorf("idpClients under asgardeo = %v", ids)
	}
}

// Under idp.clients asgardeo aectl generates the four client secrets and
// imports none, and nothing of a ThunderID in the cluster is asked for.
func TestPlatformSecrets_AsgardeoClientsAreGenerated(t *testing.T) {
	setAsgardeoClientsConfig(t)
	if administersThunder() {
		t.Fatal("administersThunder is true under idp.clients asgardeo")
	}
	generated := generatedClientSecrets()
	var paths []string
	for _, g := range generated {
		paths = append(paths, g.path)
		v, err := g.generate()
		if err != nil || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(v) {
			t.Errorf("%s: generated value is not 32 bytes as hex (err %v)", g.path, err)
		}
	}
	want := []string{"aep/idp-clients/aep-api-client", "aep/idp-clients/ae-studio-internal", "aep/idp-clients/org-publisher", "aep/idp-clients/org-studio"}
	if !slices.Equal(paths, want) {
		t.Fatalf("generated client secrets %v, want %v", paths, want)
	}
	if got := importedClientSecrets(); len(got) != 0 {
		t.Errorf("imported client secrets under asgardeo: %v", got)
	}
	all := strings.Join(platformSecretPaths(), " ")
	for _, p := range want {
		if !strings.Contains(all, p) {
			t.Errorf("platformSecretPaths lacks %s, so its Secret is not synced before it is read", p)
		}
	}
	for _, list := range [][]string{platformInputPaths(), requiredOpenBaoPaths()} {
		if got := strings.Join(list, " "); strings.Contains(got, "thunder") {
			t.Errorf("a ThunderID path under asgardeo: %s", got)
		}
	}
	if err := sreIDPSupported(); err == nil {
		t.Error("sre install is not refused under idp.clients asgardeo")
	}
	if got := envIDPPlatformTrust(platformIDP{Clients: "asgardeo", Public: testAsgardeoIDP.Public}); got.Configured() {
		t.Errorf("an environment ThunderID is told to trust the Asgardeo organization: %+v", got)
	}
}

// fakeAsgardeoAdmin is an organization as ensureAsgardeoClients sees it.
type fakeAsgardeoAdmin struct {
	// secrets is the applications that exist, by client id, with the secret
	// each accepts ("" for the public client).
	secrets  map[string]string
	problems map[string][]string
	calls    []string
}

func (f *fakeAsgardeoAdmin) Find(_ context.Context, clientID string) (string, error) {
	if _, ok := f.secrets[clientID]; ok {
		return "id-of-" + clientID, nil
	}
	return "", nil
}

func (f *fakeAsgardeoAdmin) CreateMachine(_ context.Context, clientID, secret string) (string, error) {
	f.calls = append(f.calls, "create-machine "+clientID)
	f.secrets[clientID] = secret
	return "id-of-" + clientID, nil
}

func (f *fakeAsgardeoAdmin) CreateBrowser(_ context.Context, clientID, consoleURL string) (string, error) {
	f.calls = append(f.calls, "create-browser "+clientID+" "+consoleURL)
	f.secrets[clientID] = ""
	return "id-of-" + clientID, nil
}

func (f *fakeAsgardeoAdmin) Problems(_ context.Context, app asgardeo.App, _ string) ([]string, error) {
	return f.problems[app.ClientID], nil
}

func (f *fakeAsgardeoAdmin) SecretWorks(_ context.Context, clientID, secret string) (bool, error) {
	f.calls = append(f.calls, "check-secret "+clientID)
	return f.secrets[clientID] == secret, nil
}

func (f *fakeAsgardeoAdmin) RegenerateSecret(_ context.Context, appID string) (string, error) {
	clientID := strings.TrimPrefix(appID, "id-of-")
	f.calls = append(f.calls, "regenerate "+clientID)
	f.secrets[clientID] = "issued-for-" + clientID
	return f.secrets[clientID], nil
}

// asgardeoSecrets is the K8s Secrets of the four confidential clients, each
// holding the value "vault-<vault name>".
func asgardeoSecrets() map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, d := range asgardeoClients() {
		if d.clientType != "confidential" {
			continue
		}
		if out[d.secretNameOrDefault()] == nil {
			out[d.secretNameOrDefault()] = map[string]string{}
		}
		out[d.secretNameOrDefault()][d.secretKey] = "vault-" + d.vaultName
	}
	return out
}

// ensureRun runs ensureAsgardeoClients over the five clients and records
// what it reported and stored.
type ensureRun struct {
	states map[string]string
	stored map[string]string
}

func runEnsure(t *testing.T, admin *fakeAsgardeoAdmin, replace bool, storeErr error) (ensureRun, []idpClientDef, error) {
	t.Helper()
	run := ensureRun{states: map[string]string{}, stored: map[string]string{}}
	replaced, err := ensureAsgardeoClients(context.Background(), admin, asgardeoClients(), asgardeoSecrets(),
		"http://console.ae.localhost:8080", replace,
		func(_ context.Context, path, value string) error {
			if storeErr != nil {
				return storeErr
			}
			run.stored[path] = value
			return nil
		},
		func(clientID, state string) { run.states[clientID] = state })
	return run, replaced, err
}

// In an organization that has none of them, each client is created: a
// confidential one with the secret the vault holds, the console's with the
// console URL. Nothing is regenerated and nothing is written to the vault.
func TestEnsureAsgardeoClients_CreatesWhatIsMissing(t *testing.T) {
	setAsgardeoClientsConfig(t)
	admin := &fakeAsgardeoAdmin{secrets: map[string]string{}}
	run, replaced, err := runEnsure(t, admin, false, nil)
	if err != nil || len(replaced) != 0 || len(run.stored) != 0 {
		t.Fatalf("err %v, replaced %v, stored %v", err, clientIDsOf(replaced), run.stored)
	}
	for _, id := range clientIDsOf(asgardeoClients()) {
		if run.states[id] != "created" {
			t.Errorf("%s: state %q, want created", id, run.states[id])
		}
	}
	if admin.secrets["aep-publisher-default"] != "vault-org-publisher" || admin.secrets["aep-api-client"] != "vault-aep-api-client" {
		t.Errorf("a client was not created with the vault's secret: %v", admin.secrets)
	}
	if !slices.Contains(admin.calls, "create-browser aep-console-client http://console.ae.localhost:8080") {
		t.Errorf("the console's client was not created with the console URL: %v", admin.calls)
	}
	// A secret that was just given to a new application is not checked.
	for _, c := range admin.calls {
		if strings.HasPrefix(c, "check-secret") || strings.HasPrefix(c, "regenerate") {
			t.Errorf("unexpected call %q", c)
		}
	}
}

// An application that exists and accepts the vault's secret is left as it
// is. A second run changes nothing.
func TestEnsureAsgardeoClients_LeavesWhatIsRight(t *testing.T) {
	setAsgardeoClientsConfig(t)
	admin := &fakeAsgardeoAdmin{secrets: map[string]string{}}
	if _, _, err := runEnsure(t, admin, false, nil); err != nil {
		t.Fatal(err)
	}
	admin.calls = nil
	run, replaced, err := runEnsure(t, admin, true, nil)
	if err != nil || len(replaced) != 0 || len(run.stored) != 0 {
		t.Fatalf("second run: err %v, replaced %v, stored %v", err, clientIDsOf(replaced), run.stored)
	}
	for _, c := range admin.calls {
		if !strings.HasPrefix(c, "check-secret") {
			t.Errorf("second run made the call %q", c)
		}
	}
	if run.states["aep-api-client"] != "exists" || run.states["aep-console-client"] != "exists" {
		t.Errorf("states %v", run.states)
	}
}

// An application whose secret is not the vault's is a failure without the
// flag: nothing is regenerated, and the error names the client, the flag and
// the vault path of the other way out. The other clients are still set up.
func TestEnsureAsgardeoClients_AMismatchStopsWithoutTheFlag(t *testing.T) {
	setAsgardeoClientsConfig(t)
	admin := &fakeAsgardeoAdmin{secrets: map[string]string{"aep-publisher-default": "the-secret-of-another-install"}}
	run, replaced, err := runEnsure(t, admin, false, nil)
	if err == nil || len(replaced) != 0 || len(run.stored) != 0 {
		t.Fatalf("err %v, replaced %v, stored %v; want an error and no change", err, clientIDsOf(replaced), run.stored)
	}
	for _, want := range []string{"aep-publisher-default", "--replace-client-secrets", "aep/idp-clients/org-publisher"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "the-secret-of-another-install") || strings.Contains(err.Error(), "vault-org-publisher") {
		t.Errorf("the error holds a secret: %v", err)
	}
	if slices.Contains(admin.calls, "regenerate aep-publisher-default") {
		t.Error("the secret was regenerated without the flag")
	}
	if run.states["ae-studio-default"] != "created" {
		t.Errorf("the clients after the failed one were not set up: %v", run.states)
	}
}

// With the flag, Asgardeo issues a new secret and it is written over the
// client's vault path. Only the mismatched client is touched.
func TestEnsureAsgardeoClients_TheFlagReplacesAMismatchedSecret(t *testing.T) {
	setAsgardeoClientsConfig(t)
	admin := &fakeAsgardeoAdmin{secrets: map[string]string{
		"aep-publisher-default": "an-old-secret",
		"ae-studio-default":     "vault-org-studio",
	}}
	run, replaced, err := runEnsure(t, admin, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ids := clientIDsOf(replaced); !slices.Equal(ids, []string{"aep-publisher-default"}) {
		t.Fatalf("replaced = %v, want the one mismatched client", ids)
	}
	if len(run.stored) != 1 || run.stored["aep/idp-clients/org-publisher"] != "issued-for-aep-publisher-default" {
		t.Fatalf("stored = %v, want the issued secret at the client's vault path", run.stored)
	}
	if run.states["aep-publisher-default"] != "exists, secret replaced" || run.states["ae-studio-default"] != "exists" {
		t.Errorf("states %v", run.states)
	}
}

// A new secret that could not be written to the vault is an error that says
// to run again: Asgardeo holds a secret nothing else has.
func TestEnsureAsgardeoClients_AFailedVaultWriteIsReported(t *testing.T) {
	setAsgardeoClientsConfig(t)
	admin := &fakeAsgardeoAdmin{secrets: map[string]string{"aep-api-client": "an-old-secret"}}
	_, _, err := runEnsure(t, admin, true, errors.New("vault is sealed"))
	if err == nil || !strings.Contains(err.Error(), "aep-api-client") || !strings.Contains(err.Error(), "--replace-client-secrets") {
		t.Fatalf("err = %v, want one naming the client and the flag", err)
	}
	if strings.Contains(err.Error(), "issued-for-aep-api-client") {
		t.Fatalf("the error holds the issued secret: %v", err)
	}
}

// A wrong setting is reported per client and is never rewritten. Its secret
// is not checked or replaced: the application has to be corrected first.
func TestEnsureAsgardeoClients_WrongSettingsAreFailures(t *testing.T) {
	setAsgardeoClientsConfig(t)
	admin := &fakeAsgardeoAdmin{
		secrets:  map[string]string{"aep-api-client": "an-old-secret", "aep-console-client": ""},
		problems: map[string][]string{"aep-api-client": {"access token type is Default, not JWT"}, "aep-console-client": {"PKCE is not mandatory", "it is not a public client"}},
	}
	run, replaced, err := runEnsure(t, admin, true, nil)
	if err == nil || len(replaced) != 0 {
		t.Fatalf("err %v, replaced %v", err, clientIDsOf(replaced))
	}
	for _, want := range []string{"3 problem(s)", "aep-api-client: access token type is Default, not JWT", "aep-console-client: PKCE is not mandatory", "aep-console-client: it is not a public client"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if slices.Contains(admin.calls, "regenerate aep-api-client") || slices.Contains(admin.calls, "check-secret aep-api-client") {
		t.Errorf("the wrongly set up client's secret was touched: %v", admin.calls)
	}
	if _, reported := run.states["aep-api-client"]; reported {
		t.Error("the wrongly set up client was reported as set up")
	}
}

// A confidential client without its secret in the cluster stops the run: it
// cannot be created or checked.
func TestEnsureAsgardeoClients_AMissingSecretStops(t *testing.T) {
	setAsgardeoClientsConfig(t)
	secrets := asgardeoSecrets()
	delete(secrets[orgClientSecretsName], "PLATFORM_IDP_ORG_STUDIO_SECRET")
	_, err := ensureAsgardeoClients(context.Background(), &fakeAsgardeoAdmin{secrets: map[string]string{}}, asgardeoClients(), secrets,
		"http://console.ae.localhost:8080", false,
		func(context.Context, string, string) error { return nil }, func(string, string) {})
	if err == nil || !strings.Contains(err.Error(), "PLATFORM_IDP_ORG_STUDIO_SECRET") {
		t.Fatalf("err = %v, want one naming the missing key", err)
	}
}

func setIDPAdminFlags(t *testing.T, idFile, secretFile string, replace bool) {
	t.Helper()
	t.Cleanup(func() { idpAdminClientIDFile, idpAdminClientSecretFile, replaceClientSecrets = "", "", false })
	idpAdminClientIDFile, idpAdminClientSecretFile, replaceClientSecrets = idFile, secretFile, replace
}

// The admin credential comes from two files, trimmed. A missing flag, a
// missing file and an empty file are each refused, and no error holds a
// value.
func TestReadIDPAdminCredential(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	id, secret, empty := write("id", "admin-app\n"), write("secret", "  the-admin-secret \n"), write("empty", "\n")

	setIDPAdminFlags(t, id, secret, false)
	gotID, gotSecret, err := readIDPAdminCredential()
	if err != nil || gotID != "admin-app" || gotSecret != "the-admin-secret" {
		t.Fatalf("credential read wrong (err %v)", err)
	}

	for name, files := range map[string][2]string{
		"no flags":         {"", ""},
		"one flag":         {id, ""},
		"a missing file":   {id, filepath.Join(dir, "absent")},
		"an empty file":    {id, empty},
		"an empty id file": {empty, secret},
	} {
		setIDPAdminFlags(t, files[0], files[1], false)
		_, _, err := readIDPAdminCredential()
		if err == nil || !strings.Contains(err.Error(), "idp-admin-client") {
			t.Errorf("%s: err = %v, want one naming the flag", name, err)
		}
		if err != nil && strings.Contains(err.Error(), "the-admin-secret") {
			t.Errorf("%s: the error holds the secret", name)
		}
	}
}

// The admin flags are read only where aectl creates clients in Asgardeo.
// Elsewhere a flag is refused, so it cannot look as if it took effect.
func TestRefuseIDPAdminFlags(t *testing.T) {
	setAsgardeoConfig(t) // imported clients
	setIDPAdminFlags(t, "", "", false)
	if err := refuseIDPAdminFlags(); err != nil {
		t.Fatalf("no flag set: %v", err)
	}
	setIDPAdminFlags(t, "", "", true)
	if err := refuseIDPAdminFlags(); err == nil || !strings.Contains(err.Error(), "imported") {
		t.Fatalf("--replace-client-secrets with imported clients: err = %v", err)
	}
	setIDPAdminFlags(t, "some-file", "", false)
	if err := refuseIDPAdminFlags(); err == nil {
		t.Fatal("an admin file flag with imported clients: want an error")
	}
	viper.Set("idp.clients", "asgardeo")
	if err := refuseIDPAdminFlags(); err != nil {
		t.Fatalf("under asgardeo: %v", err)
	}
}

// The closing line of a run names the IdP it set up. Where aectl creates the
// clients in Asgardeo, the command it names for the clients left out carries
// the two admin file flags, without which that command stops.
func TestClientSetupOutcome_NamesAsgardeoAndItsFlags(t *testing.T) {
	if msg, complete := clientSetupOutcome(idpNameAsgardeo, nil); !complete || msg != "Asgardeo configured" {
		t.Fatalf("msg %q, complete %v", msg, complete)
	}
	skip := map[string]string{"aep-api-client": "not synced"}
	if got := syncClientsCommand(); got != "aectl platform sync-clients" {
		t.Fatalf("the command for a ThunderID install is %q", got)
	}
	setAsgardeoClientsConfig(t)
	msg, complete := clientSetupOutcome(idpNameAsgardeo, skip)
	rows := installNotReadyRows("", skip)
	for _, text := range []string{msg, rows[len(rows)-1][1]} {
		if complete || !strings.Contains(text, "--idp-admin-client-id-file") || !strings.Contains(text, "--idp-admin-client-secret-file") {
			t.Errorf("the next step lacks the admin file flags: %q", text)
		}
	}
}

// deploymentReading is a Deployment whose one container takes env from the
// Secret secretName.
func deploymentReading(name, secretName string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "wso2-aep", Name: name},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name:    name,
			EnvFrom: []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}}}},
		}}}}},
	}
}

func restartedAt(t *testing.T, c *fake.Clientset, name string) string {
	t.Helper()
	d, err := c.AppsV1().Deployments("wso2-aep").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return d.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"]
}

func defOf(t *testing.T, clientID string) idpClientDef {
	t.Helper()
	for _, d := range asgardeoClients() {
		if d.clientID == clientID {
			return d
		}
	}
	t.Fatalf("no client %s", clientID)
	return idpClientDef{}
}

// orgSecret is the Secret of the org's two clients, holding value for both.
func orgSecret(value string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "wso2-aep", Name: orgClientSecretsName},
		Data: map[string][]byte{
			"PLATFORM_IDP_ORG_PUBLISHER_SECRET": []byte(value),
			"PLATFORM_IDP_ORG_STUDIO_SECRET":    []byte(value),
		},
	}
}

func recordedChecksum(t *testing.T, c *fake.Clientset, name string) string {
	t.Helper()
	d, err := c.AppsV1().Deployments("wso2-aep").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return d.Annotations[clientSecretChecksumAnnotation(orgClientSecretsName)]
}

// setOrgSecret is the cluster catching up with the vault: the Secret takes
// value.
func setOrgSecret(t *testing.T, c *fake.Clientset, value string) {
	t.Helper()
	if _, err := c.CoreV1().Secrets("wso2-aep").Update(context.Background(), orgSecret(value), metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
}

// replaceRun is a cluster where aep-api reads the org's client Secret and
// the console reads another one, with the checksums recorded as a run
// records them before it changes anything.
func replaceRun(t *testing.T) *fake.Clientset {
	t.Helper()
	setAsgardeoClientsConfig(t)
	c := fake.NewSimpleClientset(
		orgSecret("old"),
		deploymentReading("aep-api", orgClientSecretsName),
		deploymentReading("aep-console", "some-other-secret"),
	)
	if err := recordClientSecretChecksums(context.Background(), c, "wso2-aep", []string{orgClientSecretsName}); err != nil {
		t.Fatal(err)
	}
	return c
}

// The checksum changes with a value and is a short fingerprint, not the
// value.
func TestSecretChecksum(t *testing.T) {
	a := secretChecksum(map[string][]byte{"K1": []byte("one"), "K2": []byte("two")})
	if b := secretChecksum(map[string][]byte{"K2": []byte("two"), "K1": []byte("one")}); a != b {
		t.Errorf("the checksum depends on the order of the keys: %s, %s", a, b)
	}
	if b := secretChecksum(map[string][]byte{"K1": []byte("one"), "K2": []byte("TWO")}); a == b {
		t.Error("a changed value gives the same checksum")
	}
	if b := secretChecksum(map[string][]byte{"K1": []byte("onetwo"), "K2": nil}); a == b {
		t.Error("a value moved between keys gives the same checksum")
	}
	if len(a) != 16 || strings.Contains(a, "one") {
		t.Errorf("checksum %q, want 16 characters that hold no value", a)
	}
}

// Before a run changes anything, each reader of a client Secret is recorded
// with the Secret's checksum. Recording restarts no pod, and a checksum that
// is already there is kept: it says what the pods started with.
func TestRecordClientSecretChecksums(t *testing.T) {
	c := replaceRun(t)
	want := secretChecksum(orgSecret("old").Data)
	if got := recordedChecksum(t, c, "aep-api"); got != want {
		t.Fatalf("recorded %q, want %q", got, want)
	}
	if restartedAt(t, c, "aep-api") != "" {
		t.Error("recording a checksum restarted the Deployment")
	}
	if got := recordedChecksum(t, c, "aep-console"); got != "" {
		t.Errorf("a Deployment that does not read the Secret was recorded: %q", got)
	}
	setOrgSecret(t, c, "new")
	if err := recordClientSecretChecksums(context.Background(), c, "wso2-aep", []string{orgClientSecretsName}); err != nil {
		t.Fatal(err)
	}
	if got := recordedChecksum(t, c, "aep-api"); got != want {
		t.Errorf("a recorded checksum was overwritten: %q", got)
	}
}

// After Asgardeo issued new secrets the cluster's Secrets are synced once,
// and the Deployments that read a replaced secret are restarted: a running
// pod keeps the value it started with. A Deployment that reads another
// Secret is left alone. A run that replaced nothing, on a cluster where
// nothing is stale, syncs nothing and restarts nothing.
func TestReconcileClientSecretReaders_SyncsAndRestartsTheReaders(t *testing.T) {
	c := replaceRun(t)
	ctx, names := context.Background(), []string{orgClientSecretsName}
	syncs := 0
	sync := func(context.Context) (map[string]string, error) {
		syncs++
		setOrgSecret(t, c, "new")
		return nil, nil
	}

	if err := reconcileClientSecretReaders(ctx, c, "wso2-aep", names, nil, sync); err != nil || syncs != 0 {
		t.Fatalf("nothing replaced: err %v, syncs %d", err, syncs)
	}
	if restartedAt(t, c, "aep-api") != "" {
		t.Fatal("a Deployment was restarted though no secret changed")
	}

	replaced := []idpClientDef{defOf(t, "aep-publisher-default"), defOf(t, "ae-studio-default")}
	if err := reconcileClientSecretReaders(ctx, c, "wso2-aep", names, replaced, sync); err != nil {
		t.Fatal(err)
	}
	if syncs != 1 {
		t.Errorf("synced %d times, want once", syncs)
	}
	if restartedAt(t, c, "aep-api") == "" || recordedChecksum(t, c, "aep-api") != secretChecksum(orgSecret("new").Data) {
		t.Error("the reader of the replaced secrets was not restarted and recorded with the new checksum")
	}
	if restartedAt(t, c, "aep-console") != "" {
		t.Error("a Deployment that reads another Secret was restarted")
	}
	failing := func(context.Context) (map[string]string, error) { return nil, errors.New("no cluster") }
	if err := reconcileClientSecretReaders(ctx, c, "wso2-aep", names, replaced, failing); err == nil {
		t.Error("a failed sync after a replace is not reported")
	}
}

// After a replace of an org client's secret one step is left to the
// operator: the GitHub reconnect. The warning names the client and that
// step, and does not tell the operator to restart aep-api, which the command
// does. A replaced platform client needs no warning.
func TestReplacedOrgClientWarning(t *testing.T) {
	setAsgardeoClientsConfig(t)
	if msg := replacedOrgClientWarning([]idpClientDef{defOf(t, "aep-api-client")}); msg != "" {
		t.Errorf("a platform client: %q, want no warning", msg)
	}
	msg := replacedOrgClientWarning([]idpClientDef{defOf(t, "aep-api-client"), defOf(t, "aep-publisher-default")})
	if !strings.Contains(msg, "aep-publisher-default") || !strings.Contains(msg, "reconnect GitHub") {
		t.Errorf("warning %q lacks the client or the reconnect step", msg)
	}
	if strings.Contains(msg, "restart") || strings.Contains(msg, "aep-api-client") {
		t.Errorf("warning %q tells the operator to restart, or names a platform client", msg)
	}
}

// A restart that fails leaves the Deployment recorded with the old checksum.
// The next run replaces nothing, since the vault's secret now works, and
// still restarts the Deployment: its checksum is not the Secret's.
func TestReconcileClientSecretReaders_TheNextRunCompletesAFailedRestart(t *testing.T) {
	c := replaceRun(t)
	ctx, names := context.Background(), []string{orgClientSecretsName}
	sync := func(context.Context) (map[string]string, error) { setOrgSecret(t, c, "new"); return nil, nil }

	failPatch := true
	c.PrependReactor("patch", "deployments", func(k8stesting.Action) (bool, runtime.Object, error) {
		if failPatch {
			return true, nil, errors.New("the API server is not reachable")
		}
		return false, nil, nil
	})
	err := reconcileClientSecretReaders(ctx, c, "wso2-aep", names, []idpClientDef{defOf(t, "ae-studio-default")}, sync)
	if err == nil || !strings.Contains(err.Error(), "sync-clients") {
		t.Fatalf("err = %v, want the failed restart with the command to run again", err)
	}
	if restartedAt(t, c, "aep-api") != "" || recordedChecksum(t, c, "aep-api") != secretChecksum(orgSecret("old").Data) {
		t.Fatal("the Deployment is recorded as current though its restart failed")
	}

	failPatch = false
	neverSync := func(context.Context) (map[string]string, error) {
		t.Error("the next run synced though it replaced nothing")
		return nil, nil
	}
	if err := reconcileClientSecretReaders(ctx, c, "wso2-aep", names, nil, neverSync); err != nil {
		t.Fatal(err)
	}
	if restartedAt(t, c, "aep-api") == "" || recordedChecksum(t, c, "aep-api") != secretChecksum(orgSecret("new").Data) {
		t.Error("the next run did not restart the Deployment that was left behind")
	}
}

// A replaced secret whose Secret did not sync is an error, not a success:
// Asgardeo refuses the old value and the pods still hold it. Nothing is
// restarted with the old value. The next run, once the Secret has synced,
// restarts the readers.
func TestReconcileClientSecretReaders_AnUnsyncedReplacedSecretIsAnError(t *testing.T) {
	c := replaceRun(t)
	ctx, names := context.Background(), []string{orgClientSecretsName}
	unsynced := func(context.Context) (map[string]string, error) {
		return map[string]string{orgClientSecretsName: "not refreshed in 90s"}, nil
	}
	err := reconcileClientSecretReaders(ctx, c, "wso2-aep", names, []idpClientDef{defOf(t, "ae-studio-default")}, unsynced)
	if err == nil {
		t.Fatal("an unsynced replaced secret is reported as a success")
	}
	for _, want := range []string{"ae-studio-default", orgClientSecretsName, "not refreshed in 90s", "--idp-admin-client-id-file"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if restartedAt(t, c, "aep-api") != "" {
		t.Error("the reader of an unsynced Secret was restarted with the old value")
	}

	setOrgSecret(t, c, "new") // the next run's own sync brings the value
	if err := reconcileClientSecretReaders(ctx, c, "wso2-aep", names, nil, unsynced); err != nil {
		t.Fatal(err)
	}
	if restartedAt(t, c, "aep-api") == "" {
		t.Error("the next run did not restart the reader of the Secret that has now synced")
	}
}
