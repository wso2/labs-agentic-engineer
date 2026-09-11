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

// openapi_slice.go — SliceOpenAPI: the operations a design uses, plus every
// schema they transitively reference, as a standalone OpenAPI document.
//
// A provider's published document is often megabytes (Stripe's is over six);
// no agent turn can read it and no repo should carry it per dependency. The
// contract a design commits to is the SLICE — what the design calls — and this
// is the deterministic tool that cuts it, outside any model's context, so the
// size of the source stops mattering. Provenance (the full document's hash)
// travels with the slice so the source can be re-cut when it moves.

package spec

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SliceOpenAPI keeps, from an OpenAPI 3.x document, only the operations named
// by selectors and everything they reference. A selector is an `operationId`,
// a `METHOD /path` pair (case-insensitive method), or a bare `/path` (every
// method on it). Root-level info, servers, security and every security scheme
// are always kept; tags and other components are kept only when referenced.
// The result is canonical YAML. An unknown selector is an error naming it —
// a slice that silently dropped a request would be a contract that lies.
func SliceOpenAPI(raw []byte, selectors []string) ([]byte, error) {
	if len(selectors) == 0 {
		return nil, fmt.Errorf("no operations selected")
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("not valid YAML/JSON: %w", err)
	}
	doc, _ = canonicalize(doc).(map[string]any)
	if ver, _ := doc["openapi"].(string); !strings.HasPrefix(ver, "3.") {
		return nil, fmt.Errorf("not an OpenAPI 3.x document (openapi: %q)", ver)
	}
	paths, _ := doc["paths"].(map[string]any)
	if len(paths) == 0 {
		return nil, fmt.Errorf("OpenAPI document has no paths")
	}

	// Index every operation once so a selector resolves without a rescan.
	type opRef struct{ path, method string }
	byOperationID := map[string]opRef{}
	byMethodPath := map[string]opRef{}
	byPath := map[string][]opRef{}
	for p, item := range paths {
		ops, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for m, op := range ops {
			lm := strings.ToLower(m)
			if !httpMethods[lm] {
				continue
			}
			ref := opRef{path: p, method: m}
			byMethodPath[lm+" "+p] = ref
			byPath[p] = append(byPath[p], ref)
			if o, ok := op.(map[string]any); ok {
				if id, ok := o["operationId"].(string); ok && id != "" {
					byOperationID[id] = ref
				}
			}
		}
	}

	selected := map[opRef]struct{}{}
	var unknown []string
	for _, sel := range selectors {
		s := strings.TrimSpace(sel)
		if s == "" {
			continue
		}
		switch {
		case strings.HasPrefix(s, "/"):
			refs, ok := byPath[s]
			if !ok {
				unknown = append(unknown, s)
				continue
			}
			for _, r := range refs {
				selected[r] = struct{}{}
			}
		case strings.Contains(s, " "):
			fields := strings.Fields(s)
			key := strings.ToLower(fields[0]) + " " + strings.Join(fields[1:], " ")
			r, ok := byMethodPath[key]
			if !ok {
				unknown = append(unknown, s)
				continue
			}
			selected[r] = struct{}{}
		default:
			r, ok := byOperationID[s]
			if !ok {
				unknown = append(unknown, s)
				continue
			}
			selected[r] = struct{}{}
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("operations not in the document: %s (name an operationId, \"METHOD /path\", or \"/path\")", strings.Join(unknown, ", "))
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no operations selected")
	}

	// The kept paths: only selected methods, but path-level parameters and
	// the like ride along with any kept method.
	keptPaths := map[string]any{}
	for r := range selected {
		item := paths[r.path].(map[string]any)
		kept, _ := keptPaths[r.path].(map[string]any)
		if kept == nil {
			kept = map[string]any{}
			for k, v := range item {
				if !httpMethods[strings.ToLower(k)] {
					kept[k] = v
				}
			}
		}
		kept[r.method] = item[r.method]
		keptPaths[r.path] = kept
	}

	// Transitive $ref closure over components.
	components, _ := doc["components"].(map[string]any)
	keptComponents := map[string]map[string]any{}
	var visit func(node any)
	visit = func(node any) {
		switch n := node.(type) {
		case map[string]any:
			if ref, ok := n["$ref"].(string); ok && strings.HasPrefix(ref, "#/components/") {
				parts := strings.SplitN(strings.TrimPrefix(ref, "#/components/"), "/", 2)
				if len(parts) == 2 {
					section, name := parts[0], unescapeRef(parts[1])
					if group, ok := components[section].(map[string]any); ok {
						if def, ok := group[name]; ok {
							if keptComponents[section] == nil {
								keptComponents[section] = map[string]any{}
							}
							if _, seen := keptComponents[section][name]; !seen {
								keptComponents[section][name] = def
								visit(def)
							}
						}
					}
				}
			}
			for _, v := range n {
				visit(v)
			}
		case []any:
			for _, v := range n {
				visit(v)
			}
		}
	}
	visit(keptPaths)
	// Security schemes are referenced by name from `security`, not by $ref —
	// keep them all; they are small and the auth story is never optional.
	if schemes, ok := components["securitySchemes"].(map[string]any); ok && len(schemes) > 0 {
		keptComponents["securitySchemes"] = schemes
	}

	out := map[string]any{"openapi": doc["openapi"], "paths": keptPaths}
	for _, k := range []string{"info", "servers", "security", "externalDocs"} {
		if v, ok := doc[k]; ok {
			out[k] = v
		}
	}
	if len(keptComponents) > 0 {
		comps := map[string]any{}
		for section, group := range keptComponents {
			comps[section] = group
		}
		out["components"] = comps
	}
	// Tags: only the ones a kept operation uses.
	if tags, ok := doc["tags"].([]any); ok {
		used := map[string]struct{}{}
		for _, item := range keptPaths {
			for m, op := range item.(map[string]any) {
				if !httpMethods[strings.ToLower(m)] {
					continue
				}
				if o, ok := op.(map[string]any); ok {
					if ts, ok := o["tags"].([]any); ok {
						for _, t := range ts {
							if s, ok := t.(string); ok {
								used[s] = struct{}{}
							}
						}
					}
				}
			}
		}
		var keptTags []any
		for _, t := range tags {
			if tm, ok := t.(map[string]any); ok {
				if name, ok := tm["name"].(string); ok {
					if _, ok := used[name]; ok {
						keptTags = append(keptTags, t)
					}
				}
			}
		}
		if len(keptTags) > 0 {
			out["tags"] = keptTags
		}
	}

	rendered, err := yaml.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("render slice: %w", err)
	}
	return rendered, nil
}

// unescapeRef reverses JSON-pointer escaping in a $ref segment.
func unescapeRef(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~1", "/"), "~0", "~")
}
