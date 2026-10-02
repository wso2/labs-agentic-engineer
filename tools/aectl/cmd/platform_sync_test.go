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
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

type syncRecorder struct {
	seeded, nudged, setup int
	skip                  map[string]string
}

func (r *syncRecorder) steps(missing []string, seeded []seededSecret, awaitSkip map[string]string) syncClientsSteps {
	return syncClientsSteps{
		missingRequired: func(context.Context) ([]string, error) { return missing, nil },
		seed:            func(context.Context) ([]seededSecret, error) { r.seeded++; return seeded, nil },
		nudge:           func(context.Context, []string) { r.nudged++ },
		awaitSynced:     func(context.Context, []seededSecret) map[string]string { return awaitSkip },
		setupThunder: func(_ context.Context, skip map[string]string) error {
			r.setup++
			r.skip = skip
			return nil
		},
	}
}

func TestSyncClients_NothingSeededNoNudge(t *testing.T) {
	r := &syncRecorder{}
	if err := syncClients(context.Background(), r.steps(nil, nil, nil)); err != nil {
		t.Fatal(err)
	}
	if r.nudged != 0 || r.setup != 1 {
		t.Fatalf("nudged=%d setup=%d", r.nudged, r.setup)
	}
}

func TestSyncClients_SeededNudgesAndSkipsUnsynced(t *testing.T) {
	r := &syncRecorder{}
	skip := map[string]string{"ae-studio-internal-client": "not synced"}
	err := syncClients(context.Background(), r.steps(nil, []seededSecret{{"aep/thunder-clients/ae-studio-internal", "v"}}, skip))
	if err != nil {
		t.Fatal(err)
	}
	if r.nudged != 1 || r.setup != 1 || len(r.skip) != 1 {
		t.Fatalf("nudged=%d setup=%d skip=%v", r.nudged, r.setup, r.skip)
	}
}

// A wiped store must be refused before anything is written or registered.
func TestSyncClients_WipedStoreRefusesWritingNothing(t *testing.T) {
	r := &syncRecorder{}
	err := syncClients(context.Background(), r.steps([]string{"aep/postgres-password"}, nil, nil))
	if err == nil || !strings.Contains(err.Error(), "wiped") || !strings.Contains(err.Error(), "aep/postgres-password") {
		t.Fatalf("err = %v", err)
	}
	if r.seeded != 0 || r.nudged != 0 || r.setup != 0 {
		t.Fatalf("seeded=%d nudged=%d setup=%d", r.seeded, r.nudged, r.setup)
	}
}

func TestSyncClients_SeedErrorStopsBeforeThunder(t *testing.T) {
	r := &syncRecorder{}
	st := r.steps(nil, nil, nil)
	st.seed = func(context.Context) ([]seededSecret, error) { return nil, errors.New("boom") }
	if err := syncClients(context.Background(), st); err == nil || r.setup != 0 {
		t.Fatalf("err=%v setup=%d", err, r.setup)
	}
}

// A create-only write the store refuses (key appeared meanwhile) is "exists",
// not a failure, and is not reported as seeded.
func TestSeedMissingGeneratedSecrets_CASConflictIsExists(t *testing.T) {
	seeded, err := seedMissingGeneratedSecrets(
		func(string) (bool, error) { return false, nil },
		func(string, string) error { return errSecretExists })
	if err != nil || len(seeded) != 0 {
		t.Fatalf("seeded=%v err=%v", seeded, err)
	}
}

// With the AE-only Secret unavailable, every other client still registers.
func TestClientsToRegister_OptionalSecretMissing(t *testing.T) {
	secrets := map[string]map[string]string{thunderSecretsName: {"x": "y"}}
	var ids []string
	for _, d := range clientsToRegister(aepThunderClients, secrets, nil) {
		ids = append(ids, d.clientID)
	}
	if len(ids) != len(aepThunderClients)-1 {
		t.Fatalf("got %v", ids)
	}
	for _, id := range ids {
		if id == "ae-studio-internal-client" {
			t.Fatal("AE client must be skipped")
		}
	}
	skip := map[string]string{"aep-api-client": "stale"}
	for _, d := range clientsToRegister(aepThunderClients, secrets, skip) {
		if d.clientID == "aep-api-client" {
			t.Fatal("skipped client registered")
		}
	}
}

func TestGeneratedNamesMatchClientVaultNames(t *testing.T) {
	have := map[string]bool{}
	for _, d := range aepThunderClients {
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

func TestAwaitSeededSecretsSynced(t *testing.T) {
	seeded := []seededSecret{{"aep/thunder-clients/ae-studio-internal", "new-value"}}
	sec := func(v string) *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: aeStudioInternalSecretsName, Namespace: "ns"},
			Data:       map[string][]byte{"AE_STUDIO_INTERNAL_CLIENT_SECRET": []byte(v)},
		}
	}
	if skip := awaitSeededSecretsSynced(context.Background(), fake.NewSimpleClientset(sec("new-value")), "ns", seeded, time.Second); len(skip) != 0 {
		t.Fatalf("synced Secret skipped: %v", skip)
	}
	skip := awaitSeededSecretsSynced(context.Background(), fake.NewSimpleClientset(sec("stale")), "ns", seeded, 10*time.Millisecond)
	if len(skip) != 1 || skip["ae-studio-internal-client"] == "" {
		t.Fatalf("stale Secret must be skipped: %v", skip)
	}
	if skip := awaitSeededSecretsSynced(context.Background(), fake.NewSimpleClientset(), "ns", seeded, 10*time.Millisecond); len(skip) != 1 {
		t.Fatalf("missing Secret must be skipped: %v", skip)
	}
}
