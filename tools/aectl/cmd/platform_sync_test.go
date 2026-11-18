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
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

type syncRecorder struct {
	seeded, synced, postgres, setup int
	skip                            map[string]string
	order                           []string
}

func (r *syncRecorder) steps(missing []string, seeded []seededSecret, unsynced map[string]string) syncClientsSteps {
	return syncClientsSteps{
		missingRequired: func(context.Context) ([]string, error) { return missing, nil },
		seed: func(context.Context) ([]seededSecret, error) {
			r.seeded++
			r.order = append(r.order, "seed")
			return seeded, nil
		},
		syncSecrets: func(context.Context) (map[string]string, error) {
			r.synced++
			r.order = append(r.order, "sync")
			return unsynced, nil
		},
		syncPostgres: func(context.Context, map[string]string) error {
			r.postgres++
			r.order = append(r.order, "postgres")
			return nil
		},
		setupThunder: func(_ context.Context, skip map[string]string) error {
			r.setup++
			r.order = append(r.order, "thunder")
			r.skip = skip
			return nil
		},
	}
}

// ESO is always made to re-read OpenBao before anything reads a Secret, even
// with nothing seeded: a Secret may hold a value ESO is about to replace.
func TestSyncClients_SyncsSecretsBeforeReadersEvenWithNothingSeeded(t *testing.T) {
	r := &syncRecorder{}
	if err := syncClients(context.Background(), r.steps(nil, nil, nil)); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.order, ","); got != "seed,sync,postgres,thunder" {
		t.Fatalf("order = %s", got)
	}
}

// A client whose Secret did not sync is skipped, not registered stale.
func TestSyncClients_SkipsClientsOfUnsyncedSecrets(t *testing.T) {
	r := &syncRecorder{}
	unsynced := map[string]string{aeStudioInternalSecretsName: "not synced"}
	if err := syncClients(context.Background(), r.steps(nil, []seededSecret{{path: "aep/idp-clients/ae-studio-internal", value: "v"}}, unsynced)); err != nil {
		t.Fatal(err)
	}
	if r.setup != 1 || len(r.skip) != 1 || r.skip["ae-studio-internal-client"] == "" {
		t.Fatalf("setup=%d skip=%v", r.setup, r.skip)
	}
}

// A wiped store must be refused before anything is written or registered.
func TestSyncClients_WipedStoreRefusesWritingNothing(t *testing.T) {
	r := &syncRecorder{}
	err := syncClients(context.Background(), r.steps([]string{"aep/postgres-password"}, nil, nil))
	if err == nil || !strings.Contains(err.Error(), "wiped") || !strings.Contains(err.Error(), "aep/postgres-password") {
		t.Fatalf("err = %v", err)
	}
	if len(r.order) != 0 {
		t.Fatalf("ran %v", r.order)
	}
}

func TestSyncClients_SeedErrorStopsBeforeThunder(t *testing.T) {
	r := &syncRecorder{}
	st := r.steps(nil, nil, nil)
	st.seed = func(context.Context) ([]seededSecret, error) { return nil, errors.New("boom") }
	if err := syncClients(context.Background(), st); err == nil || r.setup != 0 || r.postgres != 0 {
		t.Fatalf("err=%v setup=%d postgres=%d", err, r.setup, r.postgres)
	}
}

func TestSyncClients_PostgresErrorStopsBeforeThunder(t *testing.T) {
	r := &syncRecorder{}
	st := r.steps(nil, nil, nil)
	st.syncPostgres = func(context.Context, map[string]string) error { return errors.New("boom") }
	if err := syncClients(context.Background(), st); err == nil || r.setup != 0 {
		t.Fatalf("err=%v setup=%d", err, r.setup)
	}
}

// With the AE-only Secret unavailable, every other client still registers.
func TestClientsToRegister_OptionalSecretMissing(t *testing.T) {
	secrets := map[string]map[string]string{thunderSecretsName: {"x": "y"}}
	var ids []string
	for _, d := range clientsToRegister(thunderClients(), secrets, nil) {
		ids = append(ids, d.clientID)
	}
	if len(ids) != len(thunderClients())-1 {
		t.Fatalf("got %v", ids)
	}
	for _, id := range ids {
		if id == "ae-studio-internal-client" {
			t.Fatal("AE client must be skipped")
		}
	}
	skip := map[string]string{"aep-api-client": "stale"}
	for _, d := range clientsToRegister(thunderClients(), secrets, skip) {
		if d.clientID == "aep-api-client" {
			t.Fatal("skipped client registered")
		}
	}
}

// A Secret that only skipped clients read is not waited for: the wait would
// stop the clients that can register. A Secret an unskipped client reads is
// still waited for.
func TestThunderSecretNames_LeavesOutASecretOnlySkippedClientsRead(t *testing.T) {
	all := thunderSecretNames(nil)
	for _, want := range []string{thunderSecretsName, aeStudioInternalSecretsName, orgClientSecretsName} {
		if !slices.Contains(all, want) {
			t.Fatalf("with nothing skipped, %s is missing from %v", want, all)
		}
	}

	// Both org clients skipped, as when their Secret did not sync.
	skip := clientsReadingUnsynced(map[string]string{orgClientSecretsName: "not synced"})
	if len(skip) != 2 {
		t.Fatalf("skip = %v, want the org's two clients", skip)
	}
	got := thunderSecretNames(skip)
	if slices.Contains(got, orgClientSecretsName) {
		t.Errorf("%s is waited for although every client that reads it is skipped: %v", orgClientSecretsName, got)
	}
	if !slices.Contains(got, thunderSecretsName) {
		t.Errorf("%s is no longer waited for: %v", thunderSecretsName, got)
	}

	// One org client skipped: the other still reads the Secret.
	one := map[string]string{"aep-publisher-" + ocOrgNamespace(): "stale"}
	if !slices.Contains(thunderSecretNames(one), orgClientSecretsName) {
		t.Errorf("%s must be waited for while one client that reads it registers", orgClientSecretsName)
	}
}

// Only an org client secret that was generated, and not restored from a
// cluster Secret, leaves a connected org with the previous value.
func TestRegeneratedOrgClientSecrets(t *testing.T) {
	seeded := []seededSecret{
		{path: "aep/idp-clients/org-publisher"},                              // generated
		{path: "aep/idp-clients/org-studio", from: "aep-org-client-secrets"}, // restored
		{path: "aep/idp-clients/aep-api-client"},                             // not an org client
	}
	got := regeneratedOrgClientSecrets(seeded)
	if len(got) != 1 || got[0] != "aep/idp-clients/org-publisher" {
		t.Fatalf("got %v, want only the generated org-publisher", got)
	}
	if got := regeneratedOrgClientSecrets(nil); len(got) != 0 {
		t.Fatalf("got %v from an empty run", got)
	}
}

func TestGeneratedNamesMatchClientVaultNames(t *testing.T) {
	have := map[string]bool{}
	for _, d := range thunderClients() {
		if d.vaultName != "" {
			have[d.vaultName] = true
		}
	}
	if len(have) != len(generatedThunderClientNames) {
		t.Fatalf("%d vaultNames vs %d generated names", len(have), len(generatedThunderClientNames))
	}
	for _, n := range generatedThunderClientNames {
		if !have[n] {
			t.Errorf("no client reads generated key %s", n)
		}
	}
}

// Only 200 is present and 404 missing; anything else fails closed.
func TestPathExists_FailsClosed(t *testing.T) {
	for status, want := range map[int]struct {
		ok  bool
		err bool
	}{200: {true, false}, 404: {false, false}, 403: {false, true}, 500: {false, true}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"errors":["denied"]}`))
		}))
		s := &openBaoSession{baseURL: srv.URL, token: "t"}
		ok, err := s.pathExists(context.Background(), "aep/x")
		srv.Close()
		if ok != want.ok || (err != nil) != want.err {
			t.Errorf("status %d: ok=%v err=%v", status, ok, err)
		}
	}
}

// The org's two clients carry the client ids aep-api derives from the org's
// namespace, are placed under the org's organization unit, and read their
// secrets from the Secret aep-api reads them from.
func TestOrgThunderClients(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Set("oc.default_org_namespace", "acme")
	viper.Set("idp.org_handle", "acme-unit")

	got := map[string]thunderClientDef{}
	for _, d := range orgThunderClients() {
		got[d.clientID] = d
	}
	for id, want := range map[string]struct{ vaultName, secretKey string }{
		"aep-publisher-acme": {"org-publisher", "PLATFORM_IDP_ORG_PUBLISHER_SECRET"},
		"ae-studio-acme":     {"org-studio", "PLATFORM_IDP_ORG_STUDIO_SECRET"},
	} {
		d, ok := got[id]
		if !ok {
			t.Fatalf("no org client %s in %v", id, got)
		}
		if d.clientType != "confidential" || d.orgOUHandle != "acme-unit" {
			t.Errorf("%s: type %q under OU handle %q, want confidential under acme-unit", id, d.clientType, d.orgOUHandle)
		}
		if d.vaultName != want.vaultName || d.secretKey != want.secretKey || d.secretName != orgClientSecretsName {
			t.Errorf("%s: vault name %q, key %q in %q", id, d.vaultName, d.secretKey, d.secretName)
		}
	}
	if len(got) != 2 {
		t.Fatalf("org clients = %d, want 2", len(got))
	}
}
