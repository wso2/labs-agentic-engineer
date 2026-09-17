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
	return &orgconfig.ConfigProjection{
		LLM:         &orgconfig.LLMProjection{Kind: "anthropic"},
		CodingLLM:   &orgconfig.LLMProjection{Kind: "anthropic"},
		GitProvider: &orgconfig.GitProviderProjection{Kind: "github"},
		IDP:         orgconfig.IDPProjection{Kind: "platform"},
		CodingAgent: orgconfig.CodingAgentProjection{
			Runtime:   "claude-code",
			Model:     "claude-sonnet-5",
			UpdatedAt: &setAt,
			UpdatedBy: &setBy,
		},
	}
}

func TestRedactConfigForPermissions_BothHeld_NothingRedacted(t *testing.T) {
	proj := fullConfigProjection()
	RedactConfigForPermissions(proj, []authz.Permission{authz.PermissionGitHubConfig, authz.PermissionModelConfig})

	if proj.GitProvider == nil {
		t.Fatal("gitProvider must survive holding ae:github-config")
	}
	if proj.LLM == nil || proj.CodingLLM == nil {
		t.Fatal("llm/codingLlm must survive holding ae:model-config")
	}
	if proj.CodingAgent.UpdatedBy == nil || proj.CodingAgent.UpdatedAt == nil {
		t.Fatal("codingAgent's audit fields must survive holding ae:model-config")
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
	if proj.CodingLLM != nil {
		t.Fatalf("codingLlm must be redacted without ae:model-config, got %+v", proj.CodingLLM)
	}
}

func TestRedactConfigForPermissions_ModelConfigOnly_GitProviderRedacted(t *testing.T) {
	proj := fullConfigProjection()
	RedactConfigForPermissions(proj, []authz.Permission{authz.PermissionModelConfig})

	if proj.LLM == nil || proj.CodingLLM == nil {
		t.Fatal("llm/codingLlm must survive holding ae:model-config")
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
	if proj.LLM != nil || proj.CodingLLM != nil {
		t.Fatalf("llm/codingLlm must be redacted holding no permission, got llm=%+v codingLlm=%+v", proj.LLM, proj.CodingLLM)
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

// TestRedactConfigForPermissions_CodingAgentAuditFieldsNeedModelConfig pins the
// trim: runtime/model stay (enum-constrained state the console renders, and the
// section is always present by contract), but updatedAt/updatedBy go. Those two
// are the compensating control for this endpoint's coarse RBAC, so a caller who
// cannot write the section must not learn who last did.
func TestRedactConfigForPermissions_CodingAgentAuditFieldsNeedModelConfig(t *testing.T) {
	for _, held := range [][]authz.Permission{nil, {authz.PermissionGitHubConfig}} {
		proj := fullConfigProjection()
		RedactConfigForPermissions(proj, held)

		if proj.CodingAgent.UpdatedBy != nil {
			t.Fatalf("held %v: updatedBy must be redacted without ae:model-config, got %q", held, *proj.CodingAgent.UpdatedBy)
		}
		if proj.CodingAgent.UpdatedAt != nil {
			t.Fatalf("held %v: updatedAt must be redacted without ae:model-config, got %v", held, *proj.CodingAgent.UpdatedAt)
		}
		if proj.CodingAgent.Runtime != "claude-code" || proj.CodingAgent.Model != "claude-sonnet-5" {
			t.Fatalf("held %v: runtime/model must survive — the section is always present by contract, got %+v", held, proj.CodingAgent)
		}
	}
}
