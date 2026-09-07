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

package spec

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// CollectSpec (dependency-management — the collect-dependency-spec route). The
// route resolves the {component, dep} external target, validates + normalizes
// the OpenAPI contract, and atomically commits the spec file + the design.json
// specPath edit (which records the dependency as having a stored contract).

const validOpenAPI = `openapi: 3.0.3
info:
  title: Stripe
  version: "1.0"
paths:
  /charges:
    get:
      responses:
        "200":
          description: ok
`

// fakeCommitter records the Files commit and answers CAS reads: the
// component's design.json and the root design.cell both exist (return a stable
// sha), the spec file does not yet. `writes` holds the LAST commit's batch
// (the single-commit tests' shorthand); `commitLog` keeps every batch in
// commit order for multi-commit composition tests.
type fakeCommitter struct {
	writes    []DesignFileWrite
	commitLog [][]DesignFileWrite
	commitErr error
	commits   int
}

func (f *fakeCommitter) ReadFile(_ context.Context, _, _, path string) (content, sha string, ok bool, err error) {
	if strings.HasSuffix(path, "design.json") || strings.HasSuffix(path, "design.cell") {
		return "{}", "sha-design", true, nil
	}
	return "", "", false, nil // spec file is new
}

func (f *fakeCommitter) Commit(_ context.Context, _, _ string, writes []DesignFileWrite, _ string) error {
	f.commits++
	if f.commitErr != nil {
		return f.commitErr
	}
	f.writes = writes
	f.commitLog = append(f.commitLog, writes)
	return nil
}

// collectSvc wires a design service over the given design tree + a fake
// committer.
func collectSvc(t *testing.T, depsJSON string, fc *fakeCommitter) *designService {
	t.Helper()
	svc := newService(readsFor(t, designFilesWithDeps(depsJSON)))
	svc.fileCommitter = fc
	return svc
}

func TestCollectSpec_RejectsBadSource(t *testing.T) {
	t.Parallel()
	svc := collectSvc(t, `[{"kind":"external","name":"stripe","style":"rest-api"}]`, &fakeCommitter{})

	if _, err := svc.CollectSpec(context.Background(), "acme", "web", "consumer", "stripe", nil, ""); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("neither source: want ErrInvalidSpec, got %v", err)
	}
	if _, err := svc.CollectSpec(context.Background(), "acme", "web", "consumer", "stripe", []byte(validOpenAPI), "https://x/y.yaml"); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("both sources: want ErrInvalidSpec, got %v", err)
	}
}

func TestCollectSpec_UnknownTarget(t *testing.T) {
	t.Parallel()
	svc := collectSvc(t, `[{"kind":"external","name":"stripe","style":"rest-api"}]`, &fakeCommitter{})

	if _, err := svc.CollectSpec(context.Background(), "acme", "web", "ghost", "stripe", []byte(validOpenAPI), ""); !errors.Is(err, ErrDependencyNotFound) {
		t.Fatalf("unknown component: want ErrDependencyNotFound, got %v", err)
	}
	if _, err := svc.CollectSpec(context.Background(), "acme", "web", "consumer", "ghost", []byte(validOpenAPI), ""); !errors.Is(err, ErrDependencyNotFound) {
		t.Fatalf("unknown dependency: want ErrDependencyNotFound, got %v", err)
	}
}

func TestCollectSpec_WrongKind(t *testing.T) {
	t.Parallel()
	svc := collectSvc(t, `[{"kind":"platform-resource","name":"db","resourceType":"postgres-cnpg"}]`, &fakeCommitter{})

	if _, err := svc.CollectSpec(context.Background(), "acme", "web", "consumer", "db", []byte(validOpenAPI), ""); !errors.Is(err, ErrDependencyWrongKind) {
		t.Fatalf("platform-resource dep: want ErrDependencyWrongKind, got %v", err)
	}
}

func TestCollectSpec_InvalidSpec(t *testing.T) {
	t.Parallel()
	svc := collectSvc(t, `[{"kind":"external","name":"stripe","style":"rest-api"}]`, &fakeCommitter{})

	if _, err := svc.CollectSpec(context.Background(), "acme", "web", "consumer", "stripe", []byte("not: openapi"), ""); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("non-OpenAPI doc: want ErrInvalidSpec, got %v", err)
	}
}

func TestCollectSpec_CommitsSpecAndDesignEdit(t *testing.T) {
	t.Parallel()
	fc := &fakeCommitter{}
	svc := collectSvc(t, `[{"kind":"external","name":"stripe","style":"rest-api"}]`, fc)

	specPath, err := svc.CollectSpec(context.Background(), "acme", "web", "consumer", "stripe", []byte(validOpenAPI), "")
	if err != nil {
		t.Fatalf("CollectSpec: unexpected error: %v", err)
	}
	if specPath != "dependencies/stripe.openapi.yaml" {
		t.Fatalf("specPath = %q, want dependencies/stripe.openapi.yaml", specPath)
	}
	if len(fc.writes) != 2 {
		t.Fatalf("want a 2-file atomic commit (spec + design.json), got %d writes", len(fc.writes))
	}
	var specW, designW *DesignFileWrite
	for i := range fc.writes {
		switch {
		case strings.HasSuffix(fc.writes[i].Path, "dependencies/stripe.openapi.yaml"):
			specW = &fc.writes[i]
		case strings.HasSuffix(fc.writes[i].Path, "components/consumer/design.json"):
			designW = &fc.writes[i]
		}
	}
	if specW == nil {
		t.Fatal("spec file not in the commit")
	}
	if specW.Path != "specs/design/components/consumer/dependencies/stripe.openapi.yaml" {
		t.Fatalf("spec full path = %q", specW.Path)
	}
	if specW.BaseSHA != "" {
		t.Fatalf("new spec file must be a create (empty BaseSHA), got %q", specW.BaseSHA)
	}
	if !strings.Contains(specW.Content, "openapi:") {
		t.Fatalf("spec content not normalized OpenAPI: %q", specW.Content)
	}
	if designW == nil {
		t.Fatal("design.json edit not in the commit")
	}
	if designW.BaseSHA != "sha-design" {
		t.Fatalf("design.json must CAS on its read sha, got %q", designW.BaseSHA)
	}
	// The specPath edit is what records the dependency's stored contract.
	if !strings.Contains(designW.Content, `"specPath": "dependencies/stripe.openapi.yaml"`) {
		t.Fatalf("design.json did not record specPath:\n%s", designW.Content)
	}
}

func TestCollectSpec_CommitConflict(t *testing.T) {
	t.Parallel()
	fc := &fakeCommitter{commitErr: ErrSpecCommitConflict}
	svc := collectSvc(t, `[{"kind":"external","name":"stripe","style":"rest-api"}]`, fc)

	if _, err := svc.CollectSpec(context.Background(), "acme", "web", "consumer", "stripe", []byte(validOpenAPI), ""); !errors.Is(err, ErrSpecCommitConflict) {
		t.Fatalf("stale design.json: want ErrSpecCommitConflict, got %v", err)
	}
}

func TestCollectSpec_NoCommitterWired(t *testing.T) {
	t.Parallel()
	svc := newService(readsFor(t, designFilesWithDeps(`[{"kind":"external","name":"stripe","style":"rest-api"}]`)))
	// fileCommitter left nil (degraded boot).

	if _, err := svc.CollectSpec(context.Background(), "acme", "web", "consumer", "stripe", []byte(validOpenAPI), ""); err == nil {
		t.Fatal("want an error when no commit surface is wired, got nil")
	}
}
