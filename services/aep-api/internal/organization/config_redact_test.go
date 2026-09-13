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

	"github.com/wso2/aep/aep-api/internal/authz"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// fullConfigProjection is a connected org: every section populated, so a test
// can assert on what redaction REMOVES rather than on absence it can't tell
// apart from "never connected".
func fullConfigProjection() *orgconfig.ConfigProjection {
	return &orgconfig.ConfigProjection{
		LLM:         &orgconfig.LLMProjection{Kind: "anthropic"},
		CodingLLM:   &orgconfig.LLMProjection{Kind: "anthropic"},
		GitProvider: &orgconfig.GitProviderProjection{Kind: "github"},
		IDP:         orgconfig.IDPProjection{Kind: "platform"},
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
	// idp has no permission mapped yet (matches updateConfigPermissions' own
	// documented gap on the write side) — it must stay visible regardless.
	proj := fullConfigProjection()
	RedactConfigForPermissions(proj, nil)

	if proj.IDP.Kind != "platform" {
		t.Fatalf("idp must never be redacted, got %+v", proj.IDP)
	}
}
