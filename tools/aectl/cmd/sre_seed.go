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

// sre_seed.go — the install-time SRE model connection seed `aectl sre
// install --llm-api-key-file/--llm-model[/--llm-base-url]` writes, so aep-api
// can bring the SRE agent up without a Console/API save (Task A2). aep-api
// applies it at most once per distinct seed via
// organization.SreModelConnectionService.ApplySeed
// (services/aep-api/internal/organization/sre_model_seed.go); this file only
// resolves the three values from flags and writes them into the Secret the
// platform chart's sreAgent.seed.secretName wires to aep-api's env.
package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// sreModelSeedSecretName is the fixed name of the Secret `aectl sre install`
// writes the seed into, and the value passed as the platform chart's
// sreAgent.seed.secretName so aep-api reads the same Secret.
const sreModelSeedSecretName = "sre-model-seed"

// sreModelSeed is the install-time SRE model connection candidate resolved
// from --llm-api-key-file/--llm-model/--llm-base-url.
type sreModelSeed struct {
	APIKey, Model, BaseURL string
}

// resolveSreModelSeed validates keyFile/model (must be given together, or
// neither) and reads+trims the key from keyFile. A nil seed with a nil error
// means neither flag was given: unchanged behaviour, no seed. The key is
// never accepted as a flag value — only read from a file — so it can never
// appear in `ps`, shell history, or aectl's own argv-derived logs.
func resolveSreModelSeed(keyFile, model, baseURL string) (*sreModelSeed, error) {
	if keyFile == "" && model == "" {
		return nil, nil
	}
	if keyFile == "" || model == "" {
		return nil, fmt.Errorf("--llm-api-key-file and --llm-model must be given together")
	}
	raw, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("read --llm-api-key-file %q: %w", keyFile, err)
	}
	key := strings.TrimSpace(string(raw))
	if key == "" {
		return nil, fmt.Errorf("--llm-api-key-file %q is empty", keyFile)
	}
	return &sreModelSeed{APIKey: key, Model: model, BaseURL: baseURL}, nil
}

// sreModelSeedHash is a seed's identity hash: sha256(baseURL \x00 model \x00
// apiKey) hex — the same formula as aep-api's seedHash
// (services/aep-api/internal/organization/sre_model_seed.go), so the two
// never drift apart. This is the one place aectl computes it; every caller
// (the platform chart's sreAgent.seed.hash set — Task A3) goes through here
// rather than re-deriving it. The key is only ever hashed, never logged.
func sreModelSeedHash(seed sreModelSeed) string {
	sum := sha256.Sum256([]byte(seed.BaseURL + "\x00" + seed.Model + "\x00" + seed.APIKey))
	return hex.EncodeToString(sum[:])
}

// ensureSREModelSeedSecret create-or-updates sreModelSeedSecretName in ns
// with seed's three values under apiKey/model/baseURL. Unlike
// ensureSREAgentSecret (create-only: aep-api owns the running secret's real
// content), this Secret is aectl's own — a re-run with a changed --llm-*
// seed must overwrite it so aep-api's ApplySeed sees the new values (it
// re-applies on any hash change, never on an unchanged one). The key is
// carried only through the typed corev1.Secret's Data map, never formatted
// into a log line or error.
func ensureSREModelSeedSecret(ctx context.Context, client kubernetes.Interface, ns string, seed sreModelSeed) error {
	data := map[string][]byte{
		"apiKey":  []byte(seed.APIKey),
		"model":   []byte(seed.Model),
		"baseURL": []byte(seed.BaseURL),
	}
	existing, err := client.CoreV1().Secrets(ns).Get(ctx, sreModelSeedSecretName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get %s secret: %w", sreModelSeedSecretName, err)
		}
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: sreModelSeedSecretName, Namespace: ns},
			Type:       corev1.SecretTypeOpaque,
			Data:       data,
		}
		if _, err := client.CoreV1().Secrets(ns).Create(ctx, sec, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create %s secret: %w", sreModelSeedSecretName, err)
		}
		return nil
	}
	existing.Type = corev1.SecretTypeOpaque
	existing.Data = data
	existing.StringData = nil
	if _, err := client.CoreV1().Secrets(ns).Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update %s secret: %w", sreModelSeedSecretName, err)
	}
	return nil
}
