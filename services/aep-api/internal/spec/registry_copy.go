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

// registry_copy.go — the platform's copy of a Registered External resource
// into a project, made at the design write (FilesService.Apply, the single
// specs/ write chokepoint).
//
// The design agent asks for a registered resource by name: it writes a STUB
// dependency.json — `{ "name": n, "resource": { "ref": n, "name": n } }` —
// and never types the keys, the provider or the organization's instructions,
// because their identities are load-bearing (the keys are the env var names
// the coding agent codes against) and a model retyping them is how they
// drift. Apply completes the stub before the commit: the record's resource
// block, with `ref` kept so a reader sees where it came from; the record's
// contract document copied beside the file byte for byte (`origin:
// registry`); and provenance naming the registry file and its hash, so a
// changed org document is detectable later (the `stale` flag) and a refresh
// is a re-copy.
//
// The registry is read BEFORE Workspace.Mutate — that fn re-runs on every CAS
// retry, and a catalog read inside it would run once per retry. The lookup
// never fails the apply: a batch is all-or-nothing over unrelated spec edits,
// so an unreachable catalog lands the stub with a warning and the dependency
// reads needs-input until the registry answers (the status ladder's rule 2);
// a name nobody registered lands the same way, with a warning that says so.

package spec

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

// RegisteredResource is one org registry record as the design write path
// reads it: the resource block in its shared shape (Contract.Path is the
// registry path, `<name>/<file>`), and the contract document's bytes.
type RegisteredResource struct {
	Resource ResourceDefinition
	// Document is the record's contract document, UTF-8; empty when the
	// record names none (a provider that publishes no document).
	Document string
}

// RegisteredResourceReader is the org registry, read for a copy. A name that
// is not a REGISTERED resource (a type a project's build left behind is not
// one) returns (nil, nil).
type RegisteredResourceReader interface {
	RegisteredResource(ctx context.Context, orgID, name string) (*RegisteredResource, error)
}

// Warning codes the copy attaches to the dependency file it could not complete.
const (
	WarningRegistryUnreachable = "registry-unreachable"
	WarningRegistryMiss        = "registry-miss"
	WarningRegistryCopied      = "registry-copied"
)

// completedFile is one dependency file the platform completed at the write:
// the dependency.json content that replaces what the agent wrote, and the
// document landed beside it (path → content) — a registry copy, or a
// provider's document fetched by URL.
type completedFile struct {
	Definition string
	Files      map[string]string
}

// completeRegistryCopies scans a batch for stub dependency files that name a
// registered resource and returns, per stub path, the completed definition
// and the document to land beside it, plus the warnings for stubs it could
// not complete. A dependency file that already carries its provider is not a
// stub and is left alone — a refresh is an explicit act, never a side effect
// of an unrelated save.
func completeRegistryCopies(ctx context.Context, reg RegisteredResourceReader, orgID string, writes []WriteOp) (map[string]completedFile, []Warning) {
	out := map[string]completedFile{}
	var warnings []Warning
	for _, w := range writes {
		dir, ok := dependencyFileDir(w.Path)
		if !ok {
			continue
		}
		def, err := parseDependencyDefinitionJSON(dir, w.Content)
		if err != nil || def.Resource.Ref == "" || def.Resource.Provider != "" {
			continue // not a stub — or not even a definition; the save gate reports that
		}
		if reg == nil {
			warnings = append(warnings, Warning{Path: w.Path, Code: WarningRegistryUnreachable,
				Message: fmt.Sprintf("the organization's resource registry is not configured, so %q could not be copied here; the dependency reads needs-input until it is", def.Name)})
			continue
		}
		rec, err := reg.RegisteredResource(ctx, orgID, def.Name)
		if err != nil {
			warnings = append(warnings, Warning{Path: w.Path, Code: WarningRegistryUnreachable,
				Message: fmt.Sprintf("the organization's resource registry could not be reached (%v), so %q could not be copied here; the dependency reads needs-input until it is", err, def.Name)})
			continue
		}
		if rec == nil {
			warnings = append(warnings, Warning{Path: w.Path, Code: WarningRegistryMiss,
				Message: fmt.Sprintf("the organization has no registered resource named %q; the dependency reads needs-input until a provider is chosen on its definition", def.Name)})
			continue
		}
		completed, files, err := renderRegistryCopy(def, *rec, time.Now().UTC())
		if err != nil {
			warnings = append(warnings, Warning{Path: w.Path, Code: WarningRegistryUnreachable,
				Message: fmt.Sprintf("the registered resource %q could not be rendered into this project: %v", def.Name, err)})
			continue
		}
		out[w.Path] = completedFile{Definition: completed, Files: files}
		// A record with no document, or one of a type a project cannot code
		// against, lands its block and no contract: say that, rather than
		// promise a document the commit does not contain.
		message := fmt.Sprintf("%q copied from the organization's registry: provider, config keys and consumption instructions landed here, and the organization holds no document to code against — the dependency reads needs-contract until one is provided", def.Name)
		if len(files) > 0 {
			message = fmt.Sprintf("%q copied from the organization's registry: provider, config keys, consumption instructions and the contract document landed beside this file", def.Name)
		}
		warnings = append(warnings, Warning{Path: w.Path, Code: WarningRegistryCopied, Message: message})
	}
	return out, warnings
}

// renderRegistryCopy builds the completed dependency.json and the document
// file for a stub, from the registry record.
func renderRegistryCopy(stub DependencyDefinition, rec RegisteredResource, now time.Time) (string, map[string]string, error) {
	if rec.Resource.Name != "" && rec.Resource.Name != stub.Name {
		return "", nil, fmt.Errorf("registry record is named %q", rec.Resource.Name)
	}
	res := rec.Resource
	res.Ref = stub.Name
	res.Name = stub.Name
	res.Provenance = nil // the org copy's own provenance stays on the record; the project's is on the dependency
	def := DependencyDefinition{Name: stub.Name, Resource: res}
	files := map[string]string{}
	if rec.Resource.Contract != nil && rec.Document != "" && StyleForContractType(rec.Resource.Contract.Type) != "" {
		// Only a document a project can code against becomes the project's
		// contract (openapi, graphql, sdk). A registered asyncapi, protobuf or
		// documentation record lands its block without one, and the dependency
		// reads needs-contract — the truth, since nothing here is a contract
		// the coding agent could build a client from.
		file := path.Base(rec.Resource.Contract.Path)
		if file == "" || file == "." || file == "/" {
			return "", nil, fmt.Errorf("registry contract path %q names no file", rec.Resource.Contract.Path)
		}
		def.Resource.Contract = &ResourceContract{Type: rec.Resource.Contract.Type, Path: file, Origin: DependencyContractOriginRegistry}
		sum := sha256.Sum256([]byte(rec.Document))
		def.Provenance = &ResourceProvenance{Registry: rec.Resource.Contract.Path, SHA256: fmt.Sprintf("%x", sum), ReadOn: now.Format(time.RFC3339)}
		files[DesignDir+"/"+dependencyDirPrefix+stub.Name+"/"+file] = rec.Document
	} else {
		def.Resource.Contract = nil
	}
	body, err := marshalDependencyDefinitionJSON(stub.Name, def)
	if err != nil {
		return "", nil, err
	}
	return string(body), files, nil
}

// dependencyFileDir returns the dependency directory name when p is a repo
// path of the form specs/design/dependencies/<name>/dependency.json.
func dependencyFileDir(p string) (string, bool) {
	prefix := DesignDir + "/" + dependencyDirPrefix
	if !strings.HasPrefix(p, prefix) || !strings.HasSuffix(p, "/"+DependencyDesignFile) {
		return "", false
	}
	rest := strings.TrimSuffix(strings.TrimPrefix(p, prefix), "/"+DependencyDesignFile)
	if rest == "" || strings.Contains(rest, "/") || p != prefix+rest+"/"+DependencyDesignFile {
		return "", false
	}
	return rest, true
}

// Warning codes the provider-document fetch attaches.
const (
	WarningProviderDocumentFetched     = "provider-document-fetched"
	WarningProviderDocumentUnavailable = "provider-document-unavailable"
)

// completeProviderDocuments is the copy's twin for a PROVIDER's published
// document. The design agent never fetches a document into its own context
// — a whole OpenAPI file can run to megabytes — so when it names one it
// writes the contract as `{ type, path, origin: provider }` with
// `provenance.sourceUrl` and no hash, and the platform fetches the document
// here (spec.FetchSpecFromURL's guards: https only, public hosts, 5 MiB),
// lands it beside the definition and fills the hash and read time. The
// empty hash is what marks the fetch as still owed: once filled, a later
// save of the same file fetches nothing. A fetch that fails lands the
// definition as written with a warning; the dependency reads
// needs-contract until the user provides the document (Provide interface)
// or the agent writes one itself.
func completeProviderDocuments(ctx context.Context, fetch func(context.Context, string) ([]byte, error), writes []WriteOp, alreadyCopied map[string]completedFile) (map[string]completedFile, []Warning) {
	out := map[string]completedFile{}
	var warnings []Warning
	inBatch := map[string]bool{}
	for _, w := range writes {
		inBatch[w.Path] = true
	}
	for _, w := range writes {
		if _, done := alreadyCopied[w.Path]; done {
			continue
		}
		dir, ok := dependencyFileDir(w.Path)
		if !ok {
			continue
		}
		def, err := parseDependencyDefinitionJSON(dir, w.Content)
		if err != nil {
			continue
		}
		c := def.Resource.Contract
		if c == nil || c.Origin != DependencyContractOriginProvider || def.Provenance == nil || def.Provenance.SourceURL == "" || def.Provenance.SHA256 != "" {
			continue
		}
		docPath := DesignDir + "/" + dependencyDirPrefix + def.Name + "/" + c.Path
		if inBatch[docPath] {
			continue // the agent landed the document itself (a small one); nothing to fetch
		}
		if fetch == nil {
			continue
		}
		body, err := fetch(ctx, def.Provenance.SourceURL)
		if err != nil {
			warnings = append(warnings, Warning{Path: w.Path, Code: WarningProviderDocumentUnavailable,
				Message: fmt.Sprintf("the provider's document at %s could not be fetched (%v); the dependency reads needs-contract until the document is provided", def.Provenance.SourceURL, err)})
			continue
		}
		if !utf8.Valid(body) {
			warnings = append(warnings, Warning{Path: w.Path, Code: WarningProviderDocumentUnavailable,
				Message: fmt.Sprintf("the provider's document at %s is not UTF-8 text; provide it as a file instead", def.Provenance.SourceURL)})
			continue
		}
		content := string(body)
		if c.Type == DependencyContractTypeOpenAPI {
			if _, verr := ValidateOpenAPI(body); verr != nil {
				warnings = append(warnings, Warning{Path: w.Path, Code: WarningProviderDocumentUnavailable,
					Message: fmt.Sprintf("the document at %s is not a valid OpenAPI document (%v); the dependency reads needs-contract until one is provided", def.Provenance.SourceURL, verr)})
				continue
			}
			if normalized, nerr := NormalizeOpenAPIYAML(content); nerr == nil {
				content = normalized
			}
		}
		sum := sha256.Sum256([]byte(content))
		def.Provenance.SHA256 = fmt.Sprintf("%x", sum)
		def.Provenance.ReadOn = time.Now().UTC().Format(time.RFC3339)
		rendered, err := marshalDependencyDefinitionJSON(def.Name, def)
		if err != nil {
			continue
		}
		out[w.Path] = completedFile{Definition: string(rendered), Files: map[string]string{docPath: content}}
		warnings = append(warnings, Warning{Path: w.Path, Code: WarningProviderDocumentFetched,
			Message: fmt.Sprintf("the provider's document was fetched from %s and landed as %s", def.Provenance.SourceURL, c.Path)})
	}
	return out, warnings
}
