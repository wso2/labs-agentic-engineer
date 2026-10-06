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

package organization

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

// storeText stands in for whatever a secret store puts in its error text; it
// must never reach a log line.
const storeText = "store-echoed-text-7f3a"

// A PAT the secret store did not accept is not saved (Q-1=C: no github-pat row,
// so GET shows no GitHub connection), so the answer is the model-key path's
// secret_store_write_failed, never "The GitHub connection was saved".
func TestSubmitFailure_PATStoreWriteFailureSaysNotSaved(t *testing.T) {
	cause := fmt.Errorf("credentials: write PAT reference: %w",
		&SecretStoreWriteError{Secret: OrgSecretGitHubPAT, Err: errors.New(storeText)})
	err := submitFailure(context.Background(), "default", "github-pat", cause)
	var se *SectionError
	if !errors.As(err, &se) {
		t.Fatalf("want a SectionError, got %#v", err)
	}
	if se.Section != "gitProvider" || se.Status != http.StatusBadGateway || se.Code != SecretStoreWriteFailedCode {
		t.Fatalf("got %s %d %q, want gitProvider 502 %q", se.Section, se.Status, se.Code, SecretStoreWriteFailedCode)
	}
	if strings.Contains(se.Message, "was saved") || !strings.Contains(se.Message, "not saved") {
		t.Fatalf("message %q must say the token was not saved", se.Message)
	}
}

// A store failure on a LATER step (the webhook secret) leaves the PAT saved, so
// the setup-incomplete answer stays true.
func TestSubmitFailure_LaterStoreWriteFailureKeepsSetupIncomplete(t *testing.T) {
	cause := &SecretStoreWriteError{Secret: OrgSecretGitHubWebhookSecret, Err: errors.New(storeText)}
	err := submitFailure(context.Background(), "default", "github-webhook-secret", cause)
	var se *SectionError
	if !errors.As(err, &se) || se.Code != AEStudioSetupIncompleteCode || se.Status != http.StatusBadGateway {
		t.Fatalf("got %#v, want 502 %q", err, AEStudioSetupIncompleteCode)
	}
}

// The failure log names an error class for a secret store failure, never the
// store's own text, like orgsecret.write_failed.
func TestSubmitFailure_StoreWriteFailureLogIsValueFree(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	cause := fmt.Errorf("credentials: write PAT reference: %w",
		&SecretStoreWriteError{Secret: OrgSecretGitHubPAT, Err: errors.New(storeText)})
	_ = submitFailure(context.Background(), "default", "github-pat", cause)
	out := logs.String()
	if strings.Contains(out, storeText) {
		t.Fatalf("log carries the store's text: %q", out)
	}
	if !strings.Contains(out, "ae_studio.gitpat_submit_failed") || !strings.Contains(out, "reason=rejected") {
		t.Fatalf("log = %q, want the event with reason=rejected", out)
	}
}
