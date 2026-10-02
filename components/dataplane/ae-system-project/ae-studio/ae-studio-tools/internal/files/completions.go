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

// The stub predicates and dependency paths are moved from
// services/aep-api/internal/spec/registry_copy.go; the aep-api copy is
// deleted in phase 4.

package files

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/wso2/aep/ae-studio-tools/internal/gen/aepapi"
	"github.com/wso2/aep/ae-studio-tools/internal/platform"
	"github.com/wso2/aep/ae-studio-tools/internal/projects"
)

// Warning codes the pod attaches to a dependency stub it could not have
// completed. aep-api's own codes (registry-copied, registry-miss,
// provider-document-fetched, …) arrive through Completer's warnings.
const (
	WarningRegistryUnreachable         = "registry-unreachable"
	WarningProviderDocumentUnavailable = "provider-document-unavailable"
)

const (
	dependenciesDir      = "specs/design/dependencies/"
	dependencyDesignFile = "dependency.json"
	// maxStubsPerCall is aep-api's cap on one completions request.
	maxStubsPerCall = 64
	// maxCompletionsBody bounds what is read of aep-api's answer.
	maxCompletionsBody = 32 << 20
)

// Completed is one dependency stub completed by aep-api: Definition replaces
// the stub's content, and Files (repo path → content) land beside it in the
// same commit.
type Completed struct {
	Definition string
	Files      map[string]string
}

// Completer completes dependency stubs on aep-api's side of the CP/DP seam,
// so a model-chosen URL is never fetched by the pod that holds the org's git
// credential (04 §4). It returns the completions keyed by stub path and
// aep-api's warnings; an error means none were made.
type Completer interface {
	Complete(ctx context.Context, project string, writes []WriteOp) (map[string]Completed, []Warning, error)
}

// stubKind is what a dependency stub is owed.
type stubKind int

const (
	// registryStub names a registered resource and types none of it.
	registryStub stubKind = iota + 1
	// providerStub names a provider's document by URL, not yet fetched.
	providerStub
)

// completeDependencies completes the save's dependency stubs before the
// commit (never inside the CAS-retried fn). Only stubs are sent; a save with
// none makes no call. A stub the completer cannot complete lands as written:
// on a failed call, or an answer the write rules refuse, each stub of that
// call gets its kind's warning and the dependency reads needs-input (or
// needs-contract) until a later save completes it.
//
//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
func (a Applier) completeDependencies(ctx context.Context, project string, writes []WriteOp) (map[string]Completed, []Warning) {
	stubs, kinds := dependencyStubs(writes)
	if len(stubs) == 0 {
		return nil, nil
	}
	completed := map[string]Completed{}
	var warnings []Warning
	for start := 0; start < len(stubs); start += maxStubsPerCall {
		chunk := stubs[start:min(start+maxStubsPerCall, len(stubs))]
		got, ws, err := a.Completer.Complete(ctx, project, chunk)
		if err == nil {
			err = validateCompletions(got, chunk)
		}
		if err != nil {
			// The error may carry aep-api's answer or a URL; log its class.
			slog.WarnContext(ctx, "files.completions_unavailable",
				"project", project, "stubs", len(chunk), "misconfigured", errors.Is(err, projects.ErrMisconfigured))
			warnings = append(warnings, unavailableWarnings(chunk, kinds)...)
			continue
		}
		for p, c := range got {
			completed[p] = c
		}
		warnings = append(warnings, ws...)
	}
	return completed, warnings
}

// unavailableWarnings is the degrade answer: each stub's own warning.
//
//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
func unavailableWarnings(stubs []WriteOp, kinds map[string]stubKind) []Warning {
	out := make([]Warning, 0, len(stubs))
	for _, s := range stubs {
		name, _ := dependencyFileDir(s.Path)
		if kinds[s.Path] == providerStub {
			out = append(out, Warning{Path: s.Path, Code: WarningProviderDocumentUnavailable,
				Message: fmt.Sprintf("the provider's document for %q could not be fetched because the platform could not be reached; the dependency reads needs-contract until the document is provided", name)})
			continue
		}
		out = append(out, Warning{Path: s.Path, Code: WarningRegistryUnreachable,
			Message: fmt.Sprintf("the organization's resource registry could not be reached, so %q could not be copied here; the dependency reads needs-input until it is", name)})
	}
	return out
}

// validateCompletions refuses an answer the pod would not write itself
// (defense in depth; aep-api refuses traversal at the producer): a completion
// for a path that was not sent, a document outside its stub's own dependency
// directory or not passing the write rules, or an oversized file.
//
//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
func validateCompletions(got map[string]Completed, sent []WriteOp) error {
	dirs := make(map[string]string, len(sent))
	for _, s := range sent {
		name, _ := dependencyFileDir(s.Path)
		dirs[s.Path] = name
	}
	for stub, c := range got {
		name, ok := dirs[stub]
		if !ok {
			return fmt.Errorf("completion for %q, which was not sent", stub)
		}
		if len(c.Definition) > maxFileBytes {
			return fmt.Errorf("completion of %q exceeds %d bytes", stub, maxFileBytes)
		}
		for p, content := range c.Files {
			if _, ok := dependencyDocumentPath(name, path.Base(p)); !ok || dependenciesDir+name+"/"+path.Base(p) != p {
				return fmt.Errorf("completion of %q lands %q outside its dependency directory", stub, p)
			}
			if len(content) > maxFileBytes {
				return fmt.Errorf("completion of %q: %q exceeds %d bytes", stub, p, maxFileBytes)
			}
		}
	}
	return nil
}

// dependencyStubs returns the writes that are dependency stubs, in request
// order, and each one's kind. A provider stub whose document the same save
// writes is not one: the agent landed the document itself.
//
//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
func dependencyStubs(writes []WriteOp) ([]WriteOp, map[string]stubKind) {
	inBatch := make(map[string]bool, len(writes))
	for _, w := range writes {
		inBatch[w.Path] = true
	}
	var stubs []WriteOp
	kinds := map[string]stubKind{}
	for _, w := range writes {
		name, ok := dependencyFileDir(w.Path)
		if !ok {
			continue
		}
		kind, docFile := classifyDependency(name, w.Content)
		if kind == providerStub {
			if doc, ok := dependencyDocumentPath(name, docFile); ok && inBatch[doc] {
				continue
			}
		}
		if kind != 0 {
			stubs = append(stubs, w)
			kinds[w.Path] = kind
		}
	}
	return stubs, kinds
}

// dependencyFields are the fields of a dependency.json the stub predicates
// read, in both the nested shape and the previous flat one.
type dependencyFields struct {
	Name     string `json:"name"`
	Source   string `json:"source"`   // flat shape: "org" names a registered resource
	Provider string `json:"provider"` // flat shape
	Resource *struct {
		Ref      string `json:"ref"`
		Name     string `json:"name"`
		Provider string `json:"provider"`
		Contract *struct {
			Path   string `json:"path"`
			Origin string `json:"origin"`
		} `json:"contract"`
	} `json:"resource"`
	Provenance *struct {
		SourceURL string `json:"sourceUrl"`
		SHA256    string `json:"sha256"`
	} `json:"provenance"`
}

// classifyDependency reports what a dependency.json in directory name is
// owed (0 for nothing) and, for a provider stub, its contract file. It
// applies aep-api's predicates (isRegistryStub, providerDocumentOwed) to the
// identity rules aep-api's parser enforces (name, resource.name and
// resource.ref equal the directory); aep-api's strict parse stays the
// authority, so a stub that passes here and fails there completes nothing.
//
//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
func classifyDependency(name, content string) (stubKind, string) {
	var probe map[string]json.RawMessage
	var def dependencyFields
	if json.Unmarshal([]byte(content), &probe) != nil || json.Unmarshal([]byte(content), &def) != nil || def.Name != name {
		return 0, ""
	}
	if _, nested := probe["resource"]; !nested {
		// The flat shape is lifted with ref = name when source is "org"; it
		// never records a provider contract origin.
		if def.Source == "org" && def.Provider == "" {
			return registryStub, ""
		}
		return 0, ""
	}
	res := def.Resource
	if res == nil || (res.Name != "" && res.Name != name) || (res.Ref != "" && res.Ref != name) {
		return 0, ""
	}
	if res.Ref != "" && res.Provider == "" {
		return registryStub, ""
	}
	if c := res.Contract; c != nil && c.Origin == "provider" && def.Provenance != nil && def.Provenance.SourceURL != "" && def.Provenance.SHA256 == "" {
		return providerStub, c.Path
	}
	return 0, ""
}

// dependencyFileDir returns the dependency directory name when p is a repo
// path of the form specs/design/dependencies/<name>/dependency.json.
//
//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
func dependencyFileDir(p string) (string, bool) {
	if !strings.HasPrefix(p, dependenciesDir) || !strings.HasSuffix(p, "/"+dependencyDesignFile) {
		return "", false
	}
	rest := strings.TrimSuffix(strings.TrimPrefix(p, dependenciesDir), "/"+dependencyDesignFile)
	if rest == "" || strings.Contains(rest, "/") || p != dependenciesDir+rest+"/"+dependencyDesignFile {
		return "", false
	}
	return rest, true
}

// dependencyDocumentPath returns the repo path of a dependency's document:
// file directly in specs/design/dependencies/<name>/, canonical, passing the
// write rules, and never the definition itself.
//
//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
func dependencyDocumentPath(name, file string) (string, bool) {
	dir := dependenciesDir + name
	p := dir + "/" + file
	if file == "" || file == dependencyDesignFile || validatePath(p) != nil || path.Dir(p) != dir {
		return "", false
	}
	return p, true
}

// NewAEPAPICompleter completes through aep-api's
// POST /internal/v1/ae-studio/dependency-completions. c carries the org's
// publisher token (platform.NewAEPAPI); aep-api answers 404 for a project
// outside the token's org. It takes the raw-op interface: the decision is
// the HTTP status, never a parsed error body.
//
//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
func NewAEPAPICompleter(c aepapi.ClientInterface) Completer {
	return aepAPICompleter{c: c}
}

type aepAPICompleter struct{ c aepapi.ClientInterface }

// Complete maps by HTTP status: 200 → the completions, 404 →
// projects.ErrUnknown, 401/403 (after the transport's one retry) or a
// rejected publisher client → projects.ErrUnavailable and ErrMisconfigured,
// anything else, a transport failure or an unusable body →
// projects.ErrUnavailable.
//
//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
func (c aepAPICompleter) Complete(ctx context.Context, project string, writes []WriteOp) (map[string]Completed, []Warning, error) {
	body := aepapi.AEStudioDependencyCompletionsRequest{Project: project, Writes: make([]aepapi.AEStudioFile, 0, len(writes))}
	for _, w := range writes {
		body.Writes = append(body.Writes, aepapi.AEStudioFile{Path: w.Path, Content: w.Content})
	}
	resp, err := c.c.CompleteAeStudioDependencies(ctx, body)
	if err != nil {
		if errors.Is(err, platform.ErrClientRejected) {
			return nil, nil, fmt.Errorf("%w: %w: %w", projects.ErrUnavailable, projects.ErrMisconfigured, err)
		}
		return nil, nil, fmt.Errorf("%w: %w", projects.ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, nil, fmt.Errorf("%w: %q", projects.ErrUnknown, project)
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, nil, fmt.Errorf("%w: %w: aep-api answered %d", projects.ErrUnavailable, projects.ErrMisconfigured, resp.StatusCode)
	default:
		return nil, nil, fmt.Errorf("%w: aep-api answered %d", projects.ErrUnavailable, resp.StatusCode)
	}
	var answer aepapi.AEStudioDependencyCompletions
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxCompletionsBody)).Decode(&answer); err != nil {
		return nil, nil, fmt.Errorf("%w: unreadable completions answer", projects.ErrUnavailable)
	}
	completed := make(map[string]Completed, len(answer.Completed))
	for _, a := range answer.Completed {
		if _, dup := completed[a.Path]; a.Path == "" || dup {
			return nil, nil, fmt.Errorf("%w: completions answer with an empty or repeated path", projects.ErrUnavailable)
		}
		files := make(map[string]string, len(a.Files))
		for _, f := range a.Files {
			files[f.Path] = f.Content
		}
		completed[a.Path] = Completed{Definition: a.Definition, Files: files}
	}
	warnings := make([]Warning, 0, len(answer.Warnings))
	for _, w := range answer.Warnings {
		warnings = append(warnings, Warning{Path: w.Path, Code: w.Code, Message: w.Message})
	}
	return completed, warnings, nil
}
