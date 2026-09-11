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

// CollectSpec / CollectDependencyContract. The route resolves the external
// dependency, validates + normalizes the OpenAPI document, and atomically
// commits it into the dependency's own directory with the dependency.json that
// records it as the contract.

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
	if strings.HasSuffix(path, "dependency.json") {
		return "{}", "sha-dependency", true, nil
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

func TestCollectSpec_CommitsContractDefinitionAndDesignEdit(t *testing.T) {
	t.Parallel()
	fc := &fakeCommitter{}
	svc := collectSvc(t, `[{"kind":"external","name":"stripe","style":"rest-api"}]`, fc)

	specPath, err := svc.CollectSpec(context.Background(), "acme", "web", "consumer", "stripe", []byte(validOpenAPI), "")
	if err != nil {
		t.Fatalf("CollectSpec: unexpected error: %v", err)
	}
	// The contract lands in the dependency's own directory, not the consumer's.
	if specPath != "specs/design/dependencies/stripe/openapi.yaml" {
		t.Fatalf("specPath = %q, want specs/design/dependencies/stripe/openapi.yaml", specPath)
	}
	if len(fc.writes) != 3 {
		t.Fatalf("want a 3-file atomic commit (contract + dependency.json + design.json), got %d writes", len(fc.writes))
	}
	var specW, defW, designW *DesignFileWrite
	for i := range fc.writes {
		switch fc.writes[i].Path {
		case "specs/design/dependencies/stripe/openapi.yaml":
			specW = &fc.writes[i]
		case "specs/design/dependencies/stripe/dependency.json":
			defW = &fc.writes[i]
		case "specs/design/components/consumer/design.json":
			designW = &fc.writes[i]
		}
	}
	if specW == nil {
		t.Fatalf("contract file not in the commit: %+v", fc.writes)
	}
	if specW.BaseSHA != "" {
		t.Fatalf("new contract file must be a create (empty BaseSHA), got %q", specW.BaseSHA)
	}
	if !strings.Contains(specW.Content, "openapi:") {
		t.Fatalf("contract content not normalized OpenAPI: %q", specW.Content)
	}
	if defW == nil {
		t.Fatalf("dependency.json not in the commit: %+v", fc.writes)
	}
	// The definition records the contract file and where it came from; that
	// is what clears the needs-contract gate on the next read.
	for _, want := range []string{`"name": "stripe"`, `"style": "rest-api"`, `"contract": "openapi.yaml"`, `"sha256": "`} {
		if !strings.Contains(defW.Content, want) {
			t.Fatalf("dependency.json did not record %s:\n%s", want, defW.Content)
		}
	}
	if designW == nil {
		t.Fatal("design.json edit not in the commit")
	}
	if designW.BaseSHA != "sha-design" {
		t.Fatalf("design.json must CAS on its read sha, got %q", designW.BaseSHA)
	}
	// The consumer's re-rendered design.json is a bare reference — the legacy
	// `style` it carried is gone (the migration rides this write).
	if strings.Contains(designW.Content, `"style"`) || strings.Contains(designW.Content, `"specPath"`) {
		t.Fatalf("design.json must not carry definition fields any more:\n%s", designW.Content)
	}
	if !strings.Contains(designW.Content, `"name": "stripe"`) {
		t.Fatalf("design.json lost the reference:\n%s", designW.Content)
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

// The dependency definition view's route: no consumer named, the definition file is the
// target, and a user-provided document replaces an assumption outright.
func TestCollectDependencyContract_WritesTheDirectoryWithoutAConsumer(t *testing.T) {
	t.Parallel()
	fc := &fakeCommitter{}
	svc := collectSvc(t, `[{"kind":"external","name":"stripe"}]`, fc)

	path, err := svc.CollectDependencyContract(context.Background(), "acme", "web", "stripe", []byte(validOpenAPI), "")
	if err != nil {
		t.Fatalf("CollectDependencyContract: %v", err)
	}
	if path != "specs/design/dependencies/stripe/openapi.yaml" {
		t.Fatalf("path = %q", path)
	}
	var defW *DesignFileWrite
	for i := range fc.writes {
		if fc.writes[i].Path == "specs/design/dependencies/stripe/dependency.json" {
			defW = &fc.writes[i]
		}
	}
	if defW == nil || !strings.Contains(defW.Content, `"contract": "openapi.yaml"`) || strings.Contains(defW.Content, `"assumed"`) {
		t.Fatalf("dependency.json = %+v", defW)
	}
	if _, err := svc.CollectDependencyContract(context.Background(), "acme", "web", "ghost", []byte(validOpenAPI), ""); !errors.Is(err, ErrDependencyNotFound) {
		t.Fatalf("unreferenced dependency: want ErrDependencyNotFound, got %v", err)
	}
}

// acceptFiles is a design whose stripe dependency carries an agent-written
// contract (marked assumed) and no acceptance yet.
func acceptFiles(marked bool) map[string]string {
	files := designFilesWithDeps(`[{"kind":"external","name":"stripe"}]`)
	files["dependencies/stripe/dependency.json"] = `{"name":"stripe","provider":"Stripe","style":"rest-api","contract":"openapi.yaml","provenance":{"sourceUrl":"https://stripe.com/docs"}}`
	contract := "openapi: 3.0.3\ninfo: {title: Stripe, version: '1'}\npaths:\n  /charges:\n    get: {responses: {'200': {description: ok}}}\n"
	if marked {
		contract = "openapi: 3.0.3\nx-aep-assumed: true\n" + contract[len("openapi: 3.0.3\n"):]
	}
	files["dependencies/stripe/openapi.yaml"] = contract
	return files
}

func TestAcceptDependencyAssumption_RecordsTheUsersPermission(t *testing.T) {
	t.Parallel()
	fc := &fakeCommitter{}
	svc := newService(readsFor(t, acceptFiles(true)))
	svc.fileCommitter = fc

	if err := svc.AcceptDependencyAssumption(context.Background(), "acme", "web", "stripe", "admin", ""); err != nil {
		t.Fatalf("AcceptDependencyAssumption: %v", err)
	}
	if len(fc.writes) != 1 || fc.writes[0].Path != "specs/design/dependencies/stripe/dependency.json" {
		t.Fatalf("writes = %+v", fc.writes)
	}
	body := fc.writes[0].Content
	for _, want := range []string{`"assumed": {`, `"by": "admin"`, `"at": "`, `"note": "Written by the design agent from https://stripe.com/docs"`, `"contract": "openapi.yaml"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("dependency.json missing %s:\n%s", want, body)
		}
	}
	// fakeCommitter reads "design.json"/"design.cell" as existing; the
	// dependency file is new to it, so the CAS is a create — fine for the
	// fake, and the real committer sees the sha it read.
}

// The record may precede the interface (the user answered "proceed on your
// assumption"; the agent writes it next), and a definition no component
// references yet is judged from its own directory.
func TestAcceptDependencyAssumption_BeforeTheInterfaceAndWithoutAConsumer(t *testing.T) {
	t.Parallel()
	// Referenced, no interface yet.
	files := designFilesWithDeps(`[{"kind":"external","name":"stripe"}]`)
	files["dependencies/stripe/dependency.json"] = `{"name":"stripe","provider":"Stripe","style":"rest-api"}`
	fc := &fakeCommitter{}
	svc := newService(readsFor(t, files))
	svc.fileCommitter = fc
	if err := svc.AcceptDependencyAssumption(context.Background(), "acme", "web", "stripe", "admin", ""); err != nil {
		t.Fatalf("before the interface: %v", err)
	}
	if len(fc.writes) != 1 || !strings.Contains(fc.writes[0].Content, `"assumed": {`) {
		t.Fatalf("writes = %+v", fc.writes)
	}
	// No consumer: an assumed interface on disk is accepted, a real one refused.
	orphan := designFilesWithDeps(`[]`)
	orphan["dependencies/printer/dependency.json"] = `{"name":"printer","provider":"Star","style":"rest-api","contract":"openapi.yaml"}`
	orphan["dependencies/printer/openapi.yaml"] = "openapi: 3.0.3\nx-aep-assumed: true\ninfo: {title: Star, version: '1'}\npaths: {}\n"
	svc2 := newService(readsFor(t, orphan))
	svc2.fileCommitter = &contractReadingCommitter{fakeCommitter: fakeCommitter{}, files: orphan}
	if err := svc2.AcceptDependencyAssumption(context.Background(), "acme", "web", "printer", "admin", ""); err != nil {
		t.Fatalf("orphan with an assumed interface: %v", err)
	}
	orphan["dependencies/printer/openapi.yaml"] = "openapi: 3.0.3\ninfo: {title: Star, version: '1'}\npaths: {}\n"
	svc3 := newService(readsFor(t, orphan))
	svc3.fileCommitter = &contractReadingCommitter{fakeCommitter: fakeCommitter{}, files: orphan}
	if err := svc3.AcceptDependencyAssumption(context.Background(), "acme", "web", "printer", "admin", ""); !errors.Is(err, ErrDependencyNotAssumed) {
		t.Fatalf("orphan with a real interface: want ErrDependencyNotAssumed, got %v", err)
	}
}

// contractReadingCommitter is a fakeCommitter that also answers ReadFile for
// the contract files the fixture holds (keyed relative to specs/design/).
type contractReadingCommitter struct {
	fakeCommitter
	files map[string]string
}

func (c *contractReadingCommitter) ReadFile(ctx context.Context, org, project, path string) (string, string, bool, error) {
	if raw, ok := c.files[strings.TrimPrefix(path, DesignDir+"/")]; ok && !strings.HasSuffix(path, "dependency.json") {
		return raw, "sha-contract", true, nil
	}
	return c.fakeCommitter.ReadFile(ctx, org, project, path)
}

func TestAcceptDependencyAssumption_RefusesAContractNobodyAssumed(t *testing.T) {
	t.Parallel()
	svc := newService(readsFor(t, acceptFiles(false)))
	svc.fileCommitter = &fakeCommitter{}
	if err := svc.AcceptDependencyAssumption(context.Background(), "acme", "web", "stripe", "admin", ""); !errors.Is(err, ErrDependencyNotAssumed) {
		t.Fatalf("want ErrDependencyNotAssumed, got %v", err)
	}
	if err := svc.AcceptDependencyAssumption(context.Background(), "acme", "web", "ghost", "admin", ""); !errors.Is(err, ErrDependencyNotFound) {
		t.Fatalf("want ErrDependencyNotFound, got %v", err)
	}
}

// A document cannot settle which system it belongs to, and an OpenAPI document
// is not a GraphQL schema: both refusals keep the file the platform writes one
// the agent's gates accept afterwards.
func TestCollectDependencyContract_DocumentNamesTheServiceAndRefusesGraphQL(t *testing.T) {
	t.Parallel()
	files := designFilesWithDeps(`[{"kind":"external","name":"mail"},{"kind":"external","name":"shop"}]`)
	files["dependencies/mail/dependency.json"] = `{"name":"mail","suggestions":[{"name":"sendgrid","style":"rest-api"},{"name":"postmark","style":"rest-api"}]}`
	files["dependencies/shop/dependency.json"] = `{"name":"shop","provider":"Shopify","style":"graphql"}`
	svc := newService(readsFor(t, files))
	fc := &fakeCommitter{}
	svc.fileCommitter = fc
	// Handing over a document is choosing: the provider is the document's
	// title and the suggestions close.
	if _, err := svc.CollectDependencyContract(context.Background(), "acme", "web", "mail", []byte(validOpenAPI), ""); err != nil {
		t.Fatalf("open suggestions: %v", err)
	}
	var defW *DesignFileWrite
	for i := range fc.writes {
		if fc.writes[i].Path == "specs/design/dependencies/mail/dependency.json" {
			defW = &fc.writes[i]
		}
	}
	if defW == nil || !strings.Contains(defW.Content, `"provider": "Stripe"`) || strings.Contains(defW.Content, `"suggestions"`) {
		t.Fatalf("dependency.json = %+v", defW)
	}
	if _, err := svc.CollectDependencyContract(context.Background(), "acme", "web", "shop", []byte(validOpenAPI), ""); !errors.Is(err, ErrDependencyWrongKind) {
		t.Fatalf("graphql: want ErrDependencyWrongKind, got %v", err)
	}
	// A document that names no system settles nothing: the platform never
	// invents a provider.
	untitled := "openapi: 3.0.3\ninfo: {version: '1'}\npaths:\n  /x:\n    get: {responses: {'200': {description: ok}}}\n"
	files2 := designFilesWithDeps(`[{"kind":"external","name":"mail"}]`)
	files2["dependencies/mail/dependency.json"] = `{"name":"mail"}`
	svc2 := newService(readsFor(t, files2))
	svc2.fileCommitter = &fakeCommitter{}
	if _, err := svc2.CollectDependencyContract(context.Background(), "acme", "web", "mail", []byte(untitled), ""); !errors.Is(err, ErrDependencyNotChosen) {
		t.Fatalf("untitled document on an unchosen dependency: want ErrDependencyNotChosen, got %v", err)
	}
}
