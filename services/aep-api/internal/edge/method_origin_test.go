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

package edge

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/wso2/aep/aep-api/internal/gen"
)

// The op→embed ledger + the reflection net guarding the edge.
//
// apiServer serves every op by promotion from its embedded domain fields, with
// ONE documented exception: the ops in edgeServed, which the edge implements
// directly (see handlers_design.go + deps.go). That makes "which embed serves
// this op?" the load-bearing question for the promoted ops, and the compiler
// only answers it in one case (a same-depth tie). These tests answer it for all
// promoted ops, and separately pin the edge-served exception set exactly.
//
// Why this reflection gate is not redundant with the compiler's own ambiguity
// error: a domain aggregator that DECLARED a handler method itself would sit at
// depth-1 and silently beat a depth-2 slice method — green build, dead slice.
// This gate asks each embed directly "do you have this method?", so it catches
// double coverage at ANY depth. It is strictly stronger. (Through P8 the ledger
// also carried the migration's legacyShim; P8 landed the last op and the shim is
// gone, so every promoted op now names its domain embed.)
//
// The edgeServed allowlist keeps the same honesty discipline: it is checked
// against the contract (no stale entry, no unlisted op) and the source-level
// gate asserts apiServer declares EXACTLY these methods — so any UNDOCUMENTED
// depth-0 method still fails loudly, and an edge-served op may not also be
// supplied by an embed.

// One const per domain — the field name of its embed in apiServer.
const (
	embedOps           = "opsHandlers"           // P1
	embedSourceControl = "sourcecontrolHandlers" // P2
	embedOrganization  = "organizationHandlers"  // P3
	embedSpec          = "specHandlers"          // P4
	embedDelivery      = "deliveryHandlers"      // P6
	embedProjects      = "projectsHandlers"      // P7
	embedDependencies  = "dependenciesHandlers"  // P8
	embedIdentity      = "identityHandlers"      // identity — the Security panel
)

// opOwner maps every operation of the committed contract to the apiServer
// embedded FIELD NAME expected to supply it. P0: every op is still legacy.
var opOwner = map[string]string{
	"ApplyFiles":                    embedSpec,
	"BuildProject":                  embedDelivery,
	"CancelRun":                     embedDelivery,
	"RevalidateBuild":               embedDelivery,
	"CollectExternalResourceValues": embedDependencies,
	"CreateIssue":                   embedSourceControl,
	"CreateProject":                 embedProjects,
	"CreateRcaAgentReport":          embedOps,
	"CreateSkill":                   embedSpec,
	"CreateTurn":                    embedSpec,
	"DeleteExternalResource":        embedDependencies,
	"DeleteProject":                 embedProjects,
	"DeleteTestUser":                embedIdentity,
	"DeleteSkill":                   embedSpec,
	"DisconnectGitProvider":         embedOrganization,
	"DiscoverIdp":                   embedOrganization,
	"GetActiveTurn":                 embedSpec,
	"GetBuildLogs":                  embedProjects,
	"GetBuildPreflight":             embedDelivery,
	"GetComponent":                  embedProjects,
	"GetComponentConfig":            embedProjects,
	"GetComponentOpenapi":           embedProjects,
	"GetConfig":                     embedOrganization,
	"GetConversation":               embedSpec,
	"GetDependencyStatus":           embedDependencies,
	"GetProjectDependencyReadiness": embedDependencies,
	"GetProject":                    embedProjects,
	"GetProjectRoles":               embedIdentity,
	"GetProjectStatus":              embedProjects,
	"GetRcaAgentReport":             embedOps,
	"GetSkill":                      embedSpec,
	"GetSpecCollabSession":          embedSpec,
	"GetTask":                       embedDelivery,
	"GetTurn":                       embedSpec,
	"ImportSkill":                   embedSpec,
	"ListAccessRequests":            embedDependencies,
	"ListActivity":                  embedProjects,
	"ListBuilds":                    embedProjects,
	"ListComponents":                embedProjects,
	"ListConversations":             embedSpec,
	"ListDeployments":               embedProjects,
	"ListExternalResources":         embedDependencies,
	"ListOrgEndpoints":              embedDependencies,
	"ListFiles":                     embedSpec,
	"ListIssues":                    embedSourceControl,
	"ListOrgEnvironments":           embedDependencies,
	"ListOrganizations":             embedOrganization,
	"ListPlatformResourceTypes":     embedDependencies,
	"ListBuildRuns":                 embedDelivery,
	"ListCycleBuilds":               embedDelivery,
	"ListProjectBuilds":             embedDelivery,
	"ListProjectTags":               embedSpec,
	"ListProjectUsage":              embedProjects,
	"ListProjects":                  embedProjects,
	"ListRcaAgentReports":           embedOps,
	"ListSkillUpdates":              embedSpec,
	"ListSkills":                    embedSpec,
	"ListTasks":                     embedDelivery,
	"ListWorkloadDependencies":      embedDependencies,
	"PromoteTaskFromIssue":          embedDelivery,
	"ProvisionPlatformResource":     embedDependencies,
	"PutProjectReferences":          embedSpec,
	"ReadFile":                      embedSpec,
	"ReadFileBundle":                embedSpec,
	"RegisterExternalResource":      embedDependencies,
	"RequestOrgServiceAccess":       embedDependencies,
	"RevealTestUserPassword":        embedIdentity,
	"RotateConversation":            embedSpec,
	"RotateTestUserPassword":        embedIdentity,
	"RotateIdpClientSecret":         embedOrganization,
	"SetSkillEnabled":               embedSpec,
	"StartGitProviderConnect":       embedOrganization,
	"StreamActivity":                embedProjects,
	"StreamBuildProgress":           embedDelivery,
	"StreamRunProgress":             embedDelivery,
	"StreamTaskLog":                 embedDelivery,
	"StreamTurn":                    embedSpec,
	"SyncSkills":                    embedSpec,
	"TriggerBuild":                  embedProjects,
	"UpdateComponentConfig":         embedProjects,
	"UpdateExternalResource":        embedDependencies,
	"UpdateConfig":                  embedOrganization,
	"UpdateSkill":                   embedSpec,
	"ValidateCollabAccess":          embedSpec,
}

// edgeServed names the ops the edge implements DIRECTLY on apiServer rather than
// promoting from a domain embed. These are the deliberate exceptions to the
// composition rule, each with a declared method + a nil-guarded 503 (see
// handlers_design.go). The spec domain deliberately exports no service interface
// (internal/spec/design_service.go), so its read-time dependency-status surface
// is wired consumer-side through edge.Deps.DesignSvc instead of a domain slice.
//
// Every entry here is EXCLUDED from the opOwner/embed requirement and is instead
// asserted to be served by apiServer's own method set (and by NO embed). The set
// is honesty-checked against the contract, exactly like opOwner.
var edgeServed = map[string]bool{
	"ListDesignDependencies": true,
}

// embedsProviding returns the names of apiServer's embedded fields whose method
// set contains op — the direct question the compiler's promotion rule answers
// only implicitly.
func embedsProviding(op string) []string {
	return embedsProvidingIn(reflect.TypeOf(apiServer{}), op)
}

// embedsProvidingIn is the detector itself, kept generic over the composite type
// so TestMethodOriginGateFires can aim it at a deliberately-broken shape and
// prove it actually reports double coverage.
func embedsProvidingIn(t reflect.Type, op string) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.Anonymous {
			continue // a named field promotes nothing
		}
		ft := f.Type
		if ft.Kind() != reflect.Pointer {
			ft = reflect.PointerTo(ft) // pointer-receiver methods are in *T's set
		}
		if _, ok := ft.MethodByName(op); ok {
			out = append(out, f.Name)
		}
	}
	sort.Strings(out)
	return out
}

// contractOps returns every operation gen.StrictServerInterface requires — the
// list is the contract's, not ours, so a new op in openapi.yaml shows up here
// the moment `make gen-api` runs.
func contractOps() []string {
	t := reflect.TypeOf((*gen.StrictServerInterface)(nil)).Elem()
	ops := make([]string, 0, t.NumMethod())
	for i := 0; i < t.NumMethod(); i++ {
		ops = append(ops, t.Method(i).Name)
	}
	sort.Strings(ops)
	return ops
}

// TestMethodOrigin asserts every contract op is supplied by EXACTLY ONE embed,
// and that it is the expected one. This is the test that fails when a migration
// adds a handler to its domain but forgets to cut the legacy one (2 providers),
// removes it from both (0 providers), or moves it without updating the ledger.
func TestMethodOrigin(t *testing.T) {
	for _, op := range contractOps() {
		if edgeServed[op] {
			// Edge-served exception: apiServer supplies it directly, so NO embed
			// may — otherwise it is a genuine double-cover between the edge body
			// and a domain slice.
			if got := embedsProviding(op); len(got) > 0 {
				t.Errorf("op %q is edge-served but also supplied by embed(s) %v — an edge-served op "+
					"must not be provided by a domain slice; drop one", op, got)
			}
			continue
		}
		want, listed := opOwner[op]
		if !listed {
			t.Errorf("op %q has no opOwner row (and is not edge-served) — add one naming the embed "+
				"that serves it", op)
			continue
		}
		got := embedsProviding(op)
		switch {
		case len(got) == 0:
			t.Errorf("op %q: NO embed supplies it (the interface cannot be satisfied)", op)
		case len(got) > 1:
			t.Errorf("op %q: DOUBLE COVERAGE — supplied by %v. The migration added it to its "+
				"domain but did not cut the legacy method; delete the legacy one.", op, got)
		case got[0] != want:
			t.Errorf("op %q: served by %q, ledger says %q — update the opOwner row in the same "+
				"commit that moves the op", op, got[0], want)
		}
	}
}

// TestOpOwnerLedgerIsHonest asserts the ledger names exactly the contract's ops
// — no stale row survives an op's removal, no new op goes unlisted. Same
// honesty discipline as internal/arch's allowlists.
func TestOpOwnerLedgerIsHonest(t *testing.T) {
	ops := map[string]bool{}
	for _, op := range contractOps() {
		ops[op] = true
	}
	for op := range opOwner {
		if !ops[op] {
			t.Errorf("opOwner names %q, which is not an operation of the contract — remove the row", op)
		}
		if edgeServed[op] {
			t.Errorf("op %q is in BOTH opOwner and edgeServed — it is served exactly one way, "+
				"pick one", op)
		}
	}
	for op := range edgeServed {
		if !ops[op] {
			t.Errorf("edgeServed names %q, which is not an operation of the contract — remove the row", op)
		}
	}
	// Every contract op is served exactly once: promoted (opOwner) or edge-served.
	if len(opOwner)+len(edgeServed) != len(ops) {
		t.Errorf("opOwner has %d rows + edgeServed has %d, but the contract has %d ops",
			len(opOwner), len(edgeServed), len(ops))
	}
}

// methodsDeclaredOn parses dir and returns the names of methods declared with a
// receiver of type recvType (value or pointer).
//
// This has to read the SOURCE. Reflection cannot answer it: a method declared on
// apiServer sits at depth-0 and shadows every embed, yet it changes nothing about
// what the embeds themselves provide — so embedsProviding still returns the
// domain embed, the ledger still matches, and every reflection assertion passes
// while the edge serves a body no embed supplied. Only the declaration site
// distinguishes "composed" from "implemented".
func methodsDeclaredOn(t *testing.T, dir, recvType string) []string {
	t.Helper()
	pkgs, err := parser.ParseDir(token.NewFileSet(), dir, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", dir, err)
	}
	var out []string
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
					continue
				}
				expr := fn.Recv.List[0].Type
				if star, isPtr := expr.(*ast.StarExpr); isPtr {
					expr = star.X
				}
				if id, ok := expr.(*ast.Ident); ok && id.Name == recvType {
					out = append(out, fn.Name.Name)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// TestApiServerDeclaresExactlyEdgeServed pins the promotion-only property the
// whole scheme rests on: apiServer COMPOSES, and implements ONLY the documented
// edgeServed ops. Any OTHER method declared on it would sit at depth-0 and
// silently beat EVERY embed, shim included — the same stale-serve failure the
// shim exists to prevent, one level up. A missing edgeServed method means the
// exception is stale.
func TestApiServerDeclaresExactlyEdgeServed(t *testing.T) {
	declared := map[string]bool{}
	for _, m := range methodsDeclaredOn(t, ".", "apiServer") {
		declared[m] = true
	}
	for m := range declared {
		if !edgeServed[m] {
			t.Errorf("apiServer declares %q, which is not in edgeServed — the edge composes, it "+
				"implements only the documented edge-served ops. An undocumented method here sits "+
				"at depth-0 and shadows every embed silently. Move the body into its domain slice, "+
				"or add it to edgeServed if it is a deliberate exception.", m)
		}
	}
	for m := range edgeServed {
		if !declared[m] {
			t.Errorf("edgeServed names %q but apiServer declares no such method — the exception is "+
				"stale; remove the edgeServed row", m)
		}
	}
}

// TestEmbedsAreConcrete pins the assumption embedsProviding rests on: every embed
// must be a struct or pointer-to-struct.
//
// An embedded INTERFACE would make the detector blind — reflect.PointerTo(iface)
// has an empty method set, so MethodByName always misses. The op would resolve
// through the interface at depth-1, the detector would report only the other
// providers, and the ledger would agree with itself while the edge served
// something else entirely.
func TestEmbedsAreConcrete(t *testing.T) {
	srv := reflect.TypeOf(apiServer{})
	for i := 0; i < srv.NumField(); i++ {
		f := srv.Field(i)
		if !f.Anonymous {
			continue
		}
		ft := f.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft.Kind() != reflect.Struct {
			t.Errorf("apiServer embeds %s (%v) — embeds must be a struct or *struct. An embedded "+
				"interface is invisible to the method-origin gate, which would then pass while "+
				"serving a body the ledger does not name.", f.Name, f.Type)
		}
	}
}

// ── Proof that the nets fire ────────────────────────────────────────────────
//
// A green gate proves nothing on its own: these plant the exact mistakes the
// migration is expected to make and assert the gate reports them.

// plantedSlice stands in for a migrated slice handler. It declares a REAL
// contract op, so the shape below is byte-for-byte the shape P1+ produces.
type plantedSlice struct{}

func (plantedSlice) ListPlatformResourceTypes(ctx context.Context, request gen.ListPlatformResourceTypesRequestObject) (gen.ListPlatformResourceTypesResponseObject, error) {
	return nil, nil
}

// plantedAggregator mirrors a domain's httpapi.Handlers: a pure aggregator that
// declares nothing and only embeds slice handlers.
type plantedAggregator struct{ plantedSlice }

// plantedLegacy mimics the PRE-CUT legacy shim: a depth-equalising wrapper that
// still declares the op. P8 was the last phase, so the REAL legacyShim now serves
// ZERO ops — there is no live op to anchor a genuine double-cover on. This fixture
// reproduces the exact shape (a promoted method reaching the composite at depth-2)
// the real shim had before its last op was cut, so the fires-proof stays honest at
// the terminal state instead of silently passing on a one-provider tie.
type plantedLegacy struct{ *plantedLegacyHandlers }

type plantedLegacyHandlers struct{}

func (*plantedLegacyHandlers) ListPlatformResourceTypes(ctx context.Context, request gen.ListPlatformResourceTypesRequestObject) (gen.ListPlatformResourceTypesResponseObject, error) {
	return nil, nil
}

// plantedDoubleCover is the migration slip: ListPlatformResourceTypes was added
// to the domain but NOT cut from legacy. Both candidates sit at depth-2, which is
// why the real apiServer would refuse to compile — so the fixture is a separate
// type, and the detector is aimed at it directly.
type plantedDoubleCover struct {
	plantedLegacy
	plantedAggregator
}

// TestMethodOriginGateFires proves the double-coverage detector reports the slip
// the compiler CANNOT catch. The compiler only rejects a same-depth tie; an
// aggregator that declared its op directly would sit at depth-1 and silently
// beat the shimmed legacy method. The reflection gate asks every embed
// regardless of depth, so it catches both.
func TestMethodOriginGateFires(t *testing.T) {
	got := embedsProvidingIn(reflect.TypeOf(plantedDoubleCover{}), "ListPlatformResourceTypes")
	if len(got) != 2 {
		t.Fatalf("planted double coverage went UNDETECTED: embedsProvidingIn = %v, want both "+
			"[plantedAggregator plantedLegacy] — the migration's headline net is not working", got)
	}
	// And it must name the culprits, not merely count them (sorted).
	if got[0] != "plantedAggregator" || got[1] != "plantedLegacy" {
		t.Fatalf("detector named %v, want [plantedAggregator plantedLegacy]", got)
	}
}

// TestMethodOriginGateAcceptsACorrectCut is the mirror: once legacy is cut, the
// op resolves to exactly one embed. Without this, a detector that always
// reported "2" would pass the test above.
func TestMethodOriginGateAcceptsACorrectCut(t *testing.T) {
	type correctCut struct {
		plantedAggregator // legacy method cut -> only the domain supplies it
	}
	if got := embedsProvidingIn(reflect.TypeOf(correctCut{}), "ListPlatformResourceTypes"); len(got) != 1 {
		t.Fatalf("a correctly-cut op reported %v, want exactly one provider", got)
	}
}

// TestApiServerDeclaresNoMethodsFires proves the source check catches the depth-0
// method — the case that defeats every reflection assertion in this file.
//
// This gap was real, not hypothetical: the first version of the check asked
// reflection whether any embed supplied the op, which a shadowing method leaves
// untouched. Planting one compiled green with all four gates passing.
func TestApiServerDeclaresNoMethodsFires(t *testing.T) {
	dir := t.TempDir()
	body := "package api\n\n" +
		"type apiServer struct{ opsHandlers }\n\n" +
		"// The shadowing method: depth-0, beats every embed, invisible to reflection.\n" +
		"func (s *apiServer) ListProjects() string { return \"shadowed\" }\n"
	if err := os.WriteFile(filepath.Join(dir, "planted.go"), []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	got := methodsDeclaredOn(t, dir, "apiServer")
	if len(got) != 1 || got[0] != "ListProjects" {
		t.Fatalf("the depth-0 detector did not fire on a planted shadowing method: got %v", got)
	}
}

// TestApiServerDeclaresNoMethodsDoesNotOverfire is the mirror: methods on OTHER
// types in the package are not apiServer's problem. Without this, a check that
// reported every method would pass the test above.
func TestApiServerDeclaresNoMethodsDoesNotOverfire(t *testing.T) {
	dir := t.TempDir()
	body := "package api\n\n" +
		"type otherHandlers struct{}\n\n" +
		"func (o *otherHandlers) ListProjects() string { return \"other\" }\n\n" +
		"type apiServer struct{ opsHandlers }\n"
	if err := os.WriteFile(filepath.Join(dir, "planted.go"), []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if got := methodsDeclaredOn(t, dir, "apiServer"); len(got) != 0 {
		t.Fatalf("the detector reported %v for methods declared on a non-apiServer type, not apiServer", got)
	}
}
