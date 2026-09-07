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

package projects

// UNIT tier: configService with a faked
// ConfigRepository (and a faked sibling ComponentService for the mirror seam) —
// no HTTP, no DB. Proves the validation branches, the DB-canonical write, the
// best-effort mirror onto OC, and the deploy-read shape. The SQL round-trip +
// org scoping run for real against Postgres in component_dbtest_test.go; the
// HTTP mapping (200-null / 400 / 500) lives in component_component_test.go.

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestConfigService_GetConfig(t *testing.T) {
	t.Parallel()
	// Repo hit → the config is returned verbatim.
	repo := &stubConfigRepo{GetByComponentFunc: func(_ context.Context, org, proj, comp string) (*ComponentConfig, error) {
		if org != "acme" || proj != "web" || comp != "svc" {
			t.Errorf("repo scope: (%q,%q,%q)", org, proj, comp)
		}
		return &ComponentConfig{OrgID: org, ProjectName: proj, ComponentName: comp, EnvVars: EnvVarSlice{{Key: "K", Value: "V"}}}, nil
	}}
	got, err := NewConfigService(repo, nil).GetConfig(context.Background(), "acme", "web", "svc")
	if err != nil || got == nil || len(got.EnvVars) != 1 {
		t.Fatalf("get happy: got=%+v err=%v", got, err)
	}

	// No row (nil,nil) is surfaced as (nil,nil) — the huma op renders it as a
	// 200 JSON null (pinned in the component tier).
	nilRepo := &stubConfigRepo{GetByComponentFunc: func(context.Context, string, string, string) (*ComponentConfig, error) {
		return nil, nil
	}}
	if got, err := NewConfigService(nilRepo, nil).GetConfig(context.Background(), "acme", "web", "svc"); err != nil || got != nil {
		t.Fatalf("no-row get must be (nil,nil), got (%+v,%v)", got, err)
	}

	// A repo error is wrapped.
	errRepo := &stubConfigRepo{GetByComponentFunc: func(context.Context, string, string, string) (*ComponentConfig, error) {
		return nil, errors.New("pg down")
	}}
	if _, err := NewConfigService(errRepo, nil).GetConfig(context.Background(), "acme", "web", "svc"); err == nil || !strings.Contains(err.Error(), "get config") {
		t.Fatalf("repo error must wrap with 'get config', got %v", err)
	}
}

func TestConfigService_UpdateConfig_Validation(t *testing.T) {
	t.Parallel()
	// Empty (whitespace-only) key is rejected BEFORE any repo write.
	repo := &stubConfigRepo{UpsertFunc: func(context.Context, *ComponentConfig) error {
		t.Error("Upsert must not run on invalid env vars")
		return nil
	}}
	svc := NewConfigService(repo, nil)
	if _, err := svc.UpdateConfig(context.Background(), "acme", "web", "svc", EnvVarSlice{{Key: "  ", Value: "v"}}); err == nil || !strings.Contains(err.Error(), "cannot be empty") {
		t.Fatalf("empty key must be rejected, got %v", err)
	}

	// Duplicate keys are rejected, naming the offending key.
	if _, err := svc.UpdateConfig(context.Background(), "acme", "web", "svc", EnvVarSlice{{Key: "DB", Value: "1"}, {Key: "DB", Value: "2"}}); err == nil || !strings.Contains(err.Error(), "duplicate environment variable key: DB") {
		t.Fatalf("duplicate key must be rejected, got %v", err)
	}
}

func TestConfigService_UpdateConfig_PersistsAndMirrors(t *testing.T) {
	t.Parallel()
	var saved *ComponentConfig
	repo := &stubConfigRepo{UpsertFunc: func(_ context.Context, c *ComponentConfig) error {
		saved = c
		return nil
	}}
	var convergedOrg, convergedProj string
	var convergedComps []string
	conv := stubConverger{fn: func(_ context.Context, org, proj string, comps []string) error {
		convergedOrg, convergedProj = org, proj
		convergedComps = comps
		return nil
	}}
	svc := NewConfigService(repo, conv)

	in := EnvVarSlice{{Key: "DB_HOST", Value: "db"}, {Key: "PORT", Value: "8080"}}
	out, err := svc.UpdateConfig(context.Background(), "acme", "web", "svc", in)
	if err != nil {
		t.Fatalf("update happy: %v", err)
	}
	// DB is canonical: the record is written scoped to (org, project, component)
	// with the supplied env vars, and returned to the caller.
	if saved == nil || saved.OrgID != "acme" || saved.ProjectName != "web" || saved.ComponentName != "svc" || len(saved.EnvVars) != 2 {
		t.Fatalf("Upsert record: %+v", saved)
	}
	if out == nil || len(out.EnvVars) != 2 {
		t.Fatalf("returned config: %+v", out)
	}
	// The edit is pushed onto the live deployment by CONVERGING that component's
	// binding — the binding has one writer, and it re-reads these values from the
	// record above rather than being handed a patch of its own.
	if convergedOrg != "acme" || convergedProj != "web" {
		t.Fatalf("converge scope: (%q,%q)", convergedOrg, convergedProj)
	}
	if len(convergedComps) != 1 || convergedComps[0] != "svc" {
		t.Fatalf("converge components: %+v", convergedComps)
	}
}

// stubConverger doubles the deployment service's converge verb.
type stubConverger struct {
	fn func(ctx context.Context, orgID, projectID string, components []string) error
}

func (s stubConverger) Converge(ctx context.Context, orgID, projectID string, components []string) error {
	if s.fn == nil {
		return nil
	}
	return s.fn(ctx, orgID, projectID, components)
}

func TestConfigService_UpdateConfig_ConvergeFailureIsBestEffort(t *testing.T) {
	t.Parallel()
	repo := &stubConfigRepo{UpsertFunc: func(context.Context, *ComponentConfig) error { return nil }}
	conv := stubConverger{fn: func(context.Context, string, string, []string) error {
		return errors.New("no release binding yet")
	}}
	// A converge failure is logged, not surfaced: the DB write already succeeded.
	out, err := NewConfigService(repo, conv).UpdateConfig(context.Background(), "acme", "web", "svc", EnvVarSlice{{Key: "K", Value: "v"}})
	if err != nil || out == nil {
		t.Fatalf("mirror failure must not fail the update: out=%+v err=%v", out, err)
	}
}

func TestConfigService_UpdateConfig_NoMirrorWhenComponentSvcNil(t *testing.T) {
	t.Parallel()
	// componentSvc nil ⇒ the mirror is skipped entirely (env vars still land in
	// the DB). A panicking stub would fire if the mirror ran.
	repo := &stubConfigRepo{UpsertFunc: func(context.Context, *ComponentConfig) error { return nil }}
	if _, err := NewConfigService(repo, nil).UpdateConfig(context.Background(), "acme", "web", "svc", EnvVarSlice{{Key: "K", Value: "v"}}); err != nil {
		t.Fatalf("nil componentSvc update: %v", err)
	}
}

func TestConfigService_UpdateConfig_RepoErrorWraps(t *testing.T) {
	t.Parallel()
	repo := &stubConfigRepo{UpsertFunc: func(context.Context, *ComponentConfig) error {
		return errors.New("unique violation")
	}}
	conv := stubConverger{fn: func(context.Context, string, string, []string) error {
		t.Error("converge must not run when the DB write failed")
		return nil
	}}
	if _, err := NewConfigService(repo, conv).UpdateConfig(context.Background(), "acme", "web", "svc", EnvVarSlice{{Key: "K", Value: "v"}}); err == nil || !strings.Contains(err.Error(), "update config") {
		t.Fatalf("Upsert error must wrap with 'update config', got %v", err)
	}
}

func TestConfigService_GetEnvVarsForDeploy(t *testing.T) {
	t.Parallel()
	// Populated config → its env vars.
	repo := &stubConfigRepo{GetByComponentFunc: func(context.Context, string, string, string) (*ComponentConfig, error) {
		return &ComponentConfig{EnvVars: EnvVarSlice{{Key: "K", Value: "V"}}}, nil
	}}
	got, err := NewConfigService(repo, nil).GetEnvVarsForDeploy(context.Background(), "acme", "web", "svc")
	if err != nil || len(got) != 1 {
		t.Fatalf("deploy env vars: got=%+v err=%v", got, err)
	}

	// No row OR an empty env list → (nil,nil): the deploy path treats "no config"
	// and "empty config" identically.
	for _, cfg := range []*ComponentConfig{nil, {EnvVars: EnvVarSlice{}}} {
		repo := &stubConfigRepo{GetByComponentFunc: func(context.Context, string, string, string) (*ComponentConfig, error) {
			return cfg, nil
		}}
		if got, err := NewConfigService(repo, nil).GetEnvVarsForDeploy(context.Background(), "acme", "web", "svc"); err != nil || got != nil {
			t.Fatalf("empty config must be (nil,nil), got (%+v,%v)", got, err)
		}
	}

	// A repo error is wrapped.
	errRepo := &stubConfigRepo{GetByComponentFunc: func(context.Context, string, string, string) (*ComponentConfig, error) {
		return nil, errors.New("pg down")
	}}
	if _, err := NewConfigService(errRepo, nil).GetEnvVarsForDeploy(context.Background(), "acme", "web", "svc"); err == nil || !strings.Contains(err.Error(), "get config for deploy") {
		t.Fatalf("repo error must wrap, got %v", err)
	}
}
