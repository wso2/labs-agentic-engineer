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
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/authz"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// fullConfigProjection is a connected org: every section populated, so a test
// can assert on what redaction REMOVES rather than on absence it can't tell
// apart from "never connected".
func fullConfigProjection() *orgconfig.ConfigProjection {
	setAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	setBy := "ada@example.com"
	disconnectedAt := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	return &orgconfig.ConfigProjection{
		LLM:               &orgconfig.LLMProjection{Kind: "anthropic"},
		LLMCheck:          &orgconfig.LLMCheck{Kind: "anthropic"},
		LLMDisconnectedAt: &disconnectedAt,
		LLMFormats:        []orgconfig.LLMFormatOption{{Kind: "anthropic"}},
		GitProvider:       &orgconfig.GitProviderProjection{Kind: "github"},
		IDP:               orgconfig.IDPProjection{Kind: "platform"},
		Agents: orgconfig.AgentsProjection{
			Runtime:           "claude-code",
			AvailableRuntimes: []orgconfig.AgentRuntime{"claude-code", "opencode"},
			Subscription:      &orgconfig.SubscriptionProjection{Kind: orgconfig.SubscriptionKindClaude},
			UpdatedAt:         &setAt,
			UpdatedBy:         &setBy,
		},
	}
}

func TestRedactConfigForPermissions_BothHeld_NothingRedacted(t *testing.T) {
	proj := fullConfigProjection()
	RedactConfigForPermissions(proj, []authz.Permission{authz.PermissionGitHubConfig, authz.PermissionModelConfig})

	if proj.GitProvider == nil {
		t.Fatal("gitProvider must survive holding ae:github-config")
	}
	if proj.LLM == nil || proj.LLMCheck == nil || proj.LLMDisconnectedAt == nil {
		t.Fatal("the model connection and its probe/disconnect detail must survive holding ae:model-config")
	}
	if proj.Agents.Subscription == nil {
		t.Fatal("agents.subscription must survive holding ae:model-config")
	}
	if proj.Agents.UpdatedBy == nil || proj.Agents.UpdatedAt == nil {
		t.Fatal("agents' audit fields must survive holding ae:model-config")
	}
}

func TestRedactConfigForPermissions_GitHubConfigOnly_LLMRedacted(t *testing.T) {
	proj := fullConfigProjection()
	RedactConfigForPermissions(proj, []authz.Permission{authz.PermissionGitHubConfig})

	if proj.GitProvider == nil {
		t.Fatal("gitProvider must survive holding ae:github-config")
	}
	if proj.LLM != nil {
		t.Fatalf("llm must be redacted without ae:model-config, got %+v", proj.LLM)
	}
	if proj.LLMCheck != nil {
		t.Fatalf("llmCheck must be redacted without ae:model-config, got %+v", proj.LLMCheck)
	}
	// Left behind, this still says the org once had a connection and lost it —
	// the very fact clearing llm is meant to withhold.
	if proj.LLMDisconnectedAt != nil {
		t.Fatalf("llmDisconnectedAt must be redacted without ae:model-config, got %v", proj.LLMDisconnectedAt)
	}
}

func TestRedactConfigForPermissions_ModelConfigOnly_GitProviderRedacted(t *testing.T) {
	proj := fullConfigProjection()
	RedactConfigForPermissions(proj, []authz.Permission{authz.PermissionModelConfig})

	if proj.LLM == nil {
		t.Fatal("llm must survive holding ae:model-config")
	}
	if proj.GitProvider != nil {
		t.Fatalf("gitProvider must be redacted without ae:github-config, got %+v", proj.GitProvider)
	}
}

func TestRedactConfigForPermissions_NeitherHeld_BothRedacted(t *testing.T) {
	proj := fullConfigProjection()
	RedactConfigForPermissions(proj, nil)

	if proj.GitProvider != nil {
		t.Fatalf("gitProvider must be redacted holding no permission, got %+v", proj.GitProvider)
	}
	if proj.LLM != nil {
		t.Fatalf("llm must be redacted holding no permission, got %+v", proj.LLM)
	}
}

func TestRedactConfigForPermissions_IDPNeverRedacted(t *testing.T) {
	// No AE permission describes identity configuration, so there is nothing to
	// redact idp against. It discloses no credential (the publisher secret is
	// never projected, only whether one exists), and its write path is refused
	// outright at the gate.
	proj := fullConfigProjection()
	RedactConfigForPermissions(proj, nil)

	if proj.IDP.Kind != "platform" {
		t.Fatalf("idp must never be redacted, got %+v", proj.IDP)
	}
}

// llmFormats is a property of the INSTALLATION — which API formats exist and
// which of this deployment's runtimes serve each — identical for every org and
// disclosing nothing about this one. It is also required by contract, so there
// is no null to clear it to.
func TestRedactConfigForPermissions_LLMFormatsNeverRedacted(t *testing.T) {
	proj := fullConfigProjection()
	RedactConfigForPermissions(proj, nil)

	if len(proj.LLMFormats) != 1 {
		t.Fatalf("llmFormats must never be redacted, got %+v", proj.LLMFormats)
	}
}

// TestRedactConfigForPermissions_AgentsTrimNeedsModelConfig pins the trim:
// runtime/availableRuntimes stay (enum-constrained state the console renders,
// and the section is always present by contract), but the stored subscription
// and updatedAt/updatedBy go. The audit pair is the compensating control for
// this endpoint's coarse RBAC, so a caller who cannot write the section must
// not learn who last did — and the subscription is a credential, masked but
// still evidence the org holds one.
func TestRedactConfigForPermissions_AgentsTrimNeedsModelConfig(t *testing.T) {
	for _, held := range [][]authz.Permission{nil, {authz.PermissionGitHubConfig}} {
		proj := fullConfigProjection()
		RedactConfigForPermissions(proj, held)

		if proj.Agents.Subscription != nil {
			t.Fatalf("held %v: subscription must be redacted without ae:model-config, got %+v", held, proj.Agents.Subscription)
		}
		if proj.Agents.UpdatedBy != nil {
			t.Fatalf("held %v: updatedBy must be redacted without ae:model-config, got %q", held, *proj.Agents.UpdatedBy)
		}
		if proj.Agents.UpdatedAt != nil {
			t.Fatalf("held %v: updatedAt must be redacted without ae:model-config, got %v", held, *proj.Agents.UpdatedAt)
		}
		if proj.Agents.Runtime != "claude-code" || len(proj.Agents.AvailableRuntimes) != 2 {
			t.Fatalf("held %v: runtime/availableRuntimes must survive — the section is always present by contract, got %+v", held, proj.Agents)
		}
	}
}
