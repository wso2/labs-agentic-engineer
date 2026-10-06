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

// The Agent Manager push's failure lines carry a reason class and AMP's
// status, never the error's text: an AMP rejection's body and a transport
// error's URL must not reach the log. Not parallel: each swaps the global
// logger.

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/agentmanager"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
	"github.com/wso2/aep/aep-api/internal/platform/patch"
)

const (
	plantedAMPToken = "planted-token-0123456789abcdef"
	plantedAMPURL   = "https://planted.example.invalid/api/v1/orgs/acme/llm-providers"
)

func captureOrgLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func wantClassedLine(t *testing.T, logs, msg string, want ...string) {
	t.Helper()
	var line string
	for _, l := range strings.Split(logs, "\n") {
		if strings.Contains(l, `"msg":"`+msg+`"`) {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no %q line in:\n%s", msg, logs)
	}
	for _, w := range want {
		if !strings.Contains(line, w) {
			t.Fatalf("line lacks %s:\n%s", w, line)
		}
	}
	if strings.Contains(logs, plantedAMPToken) || strings.Contains(logs, "planted.example.invalid") || strings.Contains(line, `"error"`) {
		t.Fatalf("raw error text reached the log:\n%s", logs)
	}
}

const (
	publishFailedMsg = "model connection: could not publish the saved connection to the Agent Manager provider"
	clearFailedMsg   = "model connection: could not clear the Agent Manager provider's copy of the disconnected key; it stays live there until cleared by hand"
)

func TestAgentManagerPush_ARejectionLogsReasonAndStatusNotText(t *testing.T) {
	logs := captureOrgLogs(t)
	f := newSaveFixture(t)
	rejected := fmt.Errorf("PUT %s (%s): %w", plantedAMPURL, plantedAMPToken, &agentmanager.PermanentError{Status: http.StatusForbidden})
	f.svc.creds.WithModelProvider(&loggingProvider{log: f.log, publishErr: rejected})

	var am *AgentManagerNotUpdatedError
	if err := f.save(connectPatch(saveKey1)); !errors.As(err, &am) {
		t.Fatalf("err = %v, want AgentManagerNotUpdatedError", err)
	}
	wantClassedLine(t, logs.String(), publishFailedMsg, `"reason":"rejected"`, `"status":403`)
}

func TestAgentManagerPush_AnUnreachableAMPLogsAClass(t *testing.T) {
	logs := captureOrgLogs(t)
	f := newSaveFixture(t)
	unreachable := &url.Error{Op: "Put", URL: plantedAMPURL + "?t=" + plantedAMPToken, Err: errors.New("connection refused")}
	f.svc.creds.WithModelProvider(&loggingProvider{log: f.log, publishErr: unreachable})

	_ = f.save(connectPatch(saveKey1))
	wantClassedLine(t, logs.String(), publishFailedMsg, `"reason":"unreachable"`)
}

func TestAgentManagerPush_AFailedClearLogsReasonAndStatusNotText(t *testing.T) {
	f := newSaveFixture(t)
	f.mustSave(connectPatch(saveKey1))
	logs := captureOrgLogs(t)
	upstream := fmt.Errorf("clear the org key on the provider: %w", &agentmanager.ServerError{Status: http.StatusBadGateway})
	f.svc.creds.WithModelProvider(&loggingProvider{log: f.log, clearErr: fmt.Errorf("%s: %w", plantedAMPURL, upstream)})

	f.mustSave(orgconfig.ConfigPatch{LLM: patch.Field[orgconfig.LLMPatch]{Sent: true, Null: true}})
	wantClassedLine(t, logs.String(), clearFailedMsg, `"reason":"upstream_error"`, `"status":502`)
}
