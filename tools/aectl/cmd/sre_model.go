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

// sre_model.go — the SRE agent's model and handoff key, both set at install
// time. `aectl sre install` writes the model, base URL and API key straight
// into the agent's own Secret (sre-agent-aep), and generates the one key the
// agent authenticates to aep-api's SRE handoff with, writing it into that
// Secret and into aep-api's (sre-handoff). Nothing in aep-api stores or pushes
// either value. Rotating the model key means re-running install with a new
// --llm-api-key-file; rotating the handoff key means --rotate-handoff-token.
package cmd

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

const (
	// sreAgentSecretName is the agent's Secret on the observability plane;
	// rca.extraEnvs reads its four keys.
	sreAgentSecretName = "sre-agent-aep"
	// sreHandoffSecretName is aep-api's copy of the handoff key, in the AEP
	// namespace; the platform chart's sreAgent.tokenSecret names it.
	sreHandoffSecretName = "sre-handoff"
	// sreHandoffTokenBytes is the handoff key's entropy before hex encoding.
	sreHandoffTokenBytes = 32
	// sreModelNamePrefix is what the stock agent's init_chat_model needs in
	// RCA_MODEL_NAME to pick its OpenAI-compatible client.
	sreModelNamePrefix = "openai:"
)

// sreModel is the SRE agent's model connection, from --llm-api-key-file,
// --llm-model and --llm-base-url.
type sreModel struct {
	APIKey, Model, BaseURL string
}

// resolveSreModel validates keyFile/model (given together, or neither) and
// reads the key from keyFile. A nil model with a nil error means neither flag
// was given, so the agent keeps the model it already has. The key is only
// ever read from a file, never taken as a flag value, so it never appears in
// `ps`, shell history or aectl's argv-derived logs. baseURL must be https:
// the key travels to it on every call.
func resolveSreModel(keyFile, model, baseURL string) (*sreModel, error) {
	if keyFile == "" && model == "" {
		return nil, nil
	}
	if keyFile == "" || model == "" {
		return nil, fmt.Errorf("--llm-api-key-file and --llm-model must be given together")
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("--llm-base-url must be an https URL, got %q", baseURL)
	}
	raw, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("read --llm-api-key-file %q: %w", keyFile, err)
	}
	key := strings.TrimSpace(string(raw))
	if key == "" {
		return nil, fmt.Errorf("--llm-api-key-file %q is empty", keyFile)
	}
	return &sreModel{APIKey: key, Model: strings.TrimPrefix(model, sreModelNamePrefix), BaseURL: strings.TrimRight(baseURL, "/")}, nil
}

// probeSreModel checks the key against the provider before anything is
// written: GET <baseURL>/models must answer 2xx. Redirects are not followed,
// so the key goes to no host but the one named. The key never appears in the
// returned error.
func probeSreModel(ctx context.Context, client *http.Client, m sreModel) error {
	probe := *client
	probe.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.BaseURL+"/models", nil)
	if err != nil {
		return fmt.Errorf("build the model probe: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.APIKey)
	resp, err := probe.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach %s to check the SRE model key: %w", m.BaseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%s rejected the SRE model key (%d); nothing was changed", m.BaseURL, resp.StatusCode)
	default:
		return fmt.Errorf("%s answered %d to the SRE model key check; nothing was changed", m.BaseURL, resp.StatusCode)
	}
}

// sreHandoffTokenHash is the handoff key's sha256, set on the platform release
// so a rotated key rolls aep-api. The key itself is only ever hashed here.
func sreHandoffTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ensureSREHandoffToken returns the handoff key held in aep-api's Secret,
// generating and writing a new one when there is none, it is malformed, or
// rotate is set. A re-run therefore keeps the key both sides already hold.
func ensureSREHandoffToken(ctx context.Context, client kubernetes.Interface, ns string, rotate bool) (string, error) {
	existing, err := client.CoreV1().Secrets(ns).Get(ctx, sreHandoffSecretName, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return "", fmt.Errorf("get %s secret: %w", sreHandoffSecretName, err)
	}
	if err == nil && !rotate {
		if token := string(existing.Data["token"]); len(token) == 2*sreHandoffTokenBytes {
			return token, nil
		}
	}
	buf := make([]byte, sreHandoffTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate the SRE handoff key: %w", err)
	}
	token := hex.EncodeToString(buf)
	if _, err := upsertSecret(ctx, client, ns, sreHandoffSecretName, map[string][]byte{"token": []byte(token)}); err != nil {
		return "", err
	}
	return token, nil
}

// sreAgentSecretState is what ensureSREAgentSecret left in the agent's Secret.
type sreAgentSecretState struct {
	// Changed says the Secret's data changed, so the agent needs a restart:
	// env read from a Secret is fixed at pod start.
	Changed bool
	// HasModel says the Secret holds a model the agent can run on.
	HasModel bool
}

// ensureSREAgentSecret writes the agent's four values: the model connection
// (m) and the handoff key. A nil m keeps the model the Secret already holds;
// on a first install without one the model keys are written empty, so the
// Secret the agent's env reads exists, and HasModel is false.
func ensureSREAgentSecret(ctx context.Context, client kubernetes.Interface, ns string, m *sreModel, token string) (sreAgentSecretState, error) {
	data := map[string][]byte{"AEP_MCP_TOKEN": []byte(token)}
	if m != nil {
		data["RCA_LLM_API_KEY"] = []byte(m.APIKey)
		data["RCA_MODEL_NAME"] = []byte(sreModelNamePrefix + m.Model)
		data["RCA_LLM_BASE_URL"] = []byte(m.BaseURL)
	} else {
		existing, err := client.CoreV1().Secrets(ns).Get(ctx, sreAgentSecretName, metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return sreAgentSecretState{}, fmt.Errorf("get %s secret: %w", sreAgentSecretName, err)
		}
		for _, k := range []string{"RCA_LLM_API_KEY", "RCA_MODEL_NAME", "RCA_LLM_BASE_URL"} {
			data[k] = nil
			if err == nil {
				data[k] = existing.Data[k]
			}
		}
	}
	changed, err := upsertSecret(ctx, client, ns, sreAgentSecretName, data)
	return sreAgentSecretState{Changed: changed, HasModel: len(data["RCA_LLM_API_KEY"]) > 0}, err
}

// upsertSecret creates or replaces the Opaque Secret name in ns with exactly
// data, and reports whether its data changed. Values travel only in the typed
// Data map, never in a log line or error.
func upsertSecret(ctx context.Context, client kubernetes.Interface, ns, name string, data map[string][]byte) (bool, error) {
	existing, err := client.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}, Type: corev1.SecretTypeOpaque, Data: data}
		if _, err := client.CoreV1().Secrets(ns).Create(ctx, sec, metav1.CreateOptions{}); err != nil {
			return false, fmt.Errorf("create %s secret: %w", name, err)
		}
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("get %s secret: %w", name, err)
	}
	if sameSecretData(existing.Data, data) {
		return false, nil
	}
	existing.Type = corev1.SecretTypeOpaque
	existing.Data = data
	existing.StringData = nil
	if _, err := client.CoreV1().Secrets(ns).Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
		return false, fmt.Errorf("update %s secret: %w", name, err)
	}
	return true, nil
}

// scaleSREAgent sets the agent Deployment's replicas: 0 while it has no model
// to run on, 1 once it has.
func scaleSREAgent(ctx context.Context, client kubernetes.Interface, ns, name string, replicas int32) error {
	d, err := client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	if d.Spec.Replicas != nil && *d.Spec.Replicas == replicas {
		return nil
	}
	patch := []byte(fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas))
	if _, err := client.AppsV1().Deployments(ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("scale %s to %d: %w", name, replicas, err)
	}
	return nil
}

func sameSecretData(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range b {
		if !bytes.Equal(a[k], v) {
			return false
		}
	}
	return true
}
