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

package codingagent

import "testing"

// validJobInputs is the minimal input set that passes validate().
func validJobInputs() JobInputs {
	return JobInputs{
		RunName:             "ca-abc123-2601011200",
		OrgNS:               "wc-org-remote-worker",
		TaskID:              "exec-1",
		OrgID:               "org1",
		ProjectID:           "proj1",
		ComponentName:       "hello-world-api",
		RunnerImage:         "aep/remote-worker:latest",
		ServiceAccountName:  "runner-sa",
		AnthropicSecretName: "anthropic-secret",
		GitHubSecretName:    "github-secret",
		RepoURL:             "https://github.com/acme/hello-world-api",
		Prompt:              "work the issue",
		IdentityName:        "AEP Bot",
		IdentityEmail:       "bot@aep.dev",
		GitServiceURL:       "http://aep-api:9090",
		CallbackURL:         "http://aep-api:9090",
	}
}

// jobEnv extracts the runner container's env list as name→value from a built
// Job manifest, failing the test if the nested shape is unexpected.
func jobEnv(t *testing.T, job map[string]any) map[string]string {
	t.Helper()
	spec, _ := job["spec"].(map[string]any)
	tmpl, _ := spec["template"].(map[string]any)
	podSpec, _ := tmpl["spec"].(map[string]any)
	containers, _ := podSpec["containers"].([]map[string]any)
	if len(containers) == 0 {
		t.Fatalf("no containers in job manifest: %+v", job)
	}
	envList, _ := containers[0]["env"].([]map[string]any)
	out := map[string]string{}
	for _, e := range envList {
		name, _ := e["name"].(string)
		val, _ := e["value"].(string)
		out[name] = val
	}
	return out
}

// TestBuild_StampsSkillsRepoURLWhenSet pins the new optional AEP_SKILLS_REPO_URL
// env: present with the clone URL when the org's skills repo resolved.
func TestBuild_StampsSkillsRepoURLWhenSet(t *testing.T) {
	in := validJobInputs()
	in.SkillsRepoURL = "https://github.com/acme/org-skills"

	job, err := Build(in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	env := jobEnv(t, job)
	if got := env["AEP_SKILLS_REPO_URL"]; got != in.SkillsRepoURL {
		t.Errorf("AEP_SKILLS_REPO_URL = %q, want %q", got, in.SkillsRepoURL)
	}
}

// TestBuild_OmitsSkillsRepoURLWhenEmpty pins the degrade contract: an
// unprovisioned org (empty URL) stamps no env var, so the runner falls back to
// the base plugin rather than cloning "".
func TestBuild_OmitsSkillsRepoURLWhenEmpty(t *testing.T) {
	in := validJobInputs() // SkillsRepoURL left empty

	job, err := Build(in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	env := jobEnv(t, job)
	if _, present := env["AEP_SKILLS_REPO_URL"]; present {
		t.Errorf("AEP_SKILLS_REPO_URL must be absent when SkillsRepoURL is empty, got %q", env["AEP_SKILLS_REPO_URL"])
	}
}
