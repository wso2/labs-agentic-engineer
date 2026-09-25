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

// dependency_json.go — the codec for an external dependency's own file,
// `specs/design/dependencies/<name>/dependency.json`, and the hydration that
// copies it onto every component edge that references it.
//
// One dependency, one definition, and the definition holds a full RESOURCE
// block in the one shape a resource has everywhere: the org registry record
// and a project's copy (or its own inline resource) are the same object. A
// component's design.json says only `{ "kind": "external", "name": "…" }`; the
// provider, the config keys, the contract and (for a copy) the organization's
// instructions live once, here, shared by every component that uses the
// dependency. The read path (AssembleDesign) hydrates each reference so
// downstream readers — wiring derivation, the build preflight, the deploy
// gate, the console — keep the flat `Dependency` they always had; the write
// path (SplitDesign) writes both halves back.
//
// What "resolved" means is on disk: a Provider, a contract file beside this
// one, the Config key names — or a Ref to a registered resource. State is
// never written (ADR-0003 stands) — ComputeDependencyStatus derives it from
// which of these are present, plus one registry lookup for a Ref.
//
// Style is not stored: it is computed from the contract type (openapi → a REST
// client, graphql → a GraphQL client, sdk → a vendor library). Nor is where a
// contract came from stored as a file marker: `contract.origin` says it, and
// the old `x-aep-derived` / `x-aep-assumed` markers are read only as a
// fallback for files written before origin existed.
//
// A file in the previous FLAT shape (provider / style / contract as a name /
// source / assumed at the top level) still decodes: liftFlatDefinition turns
// it into the nested shape in memory, so the next SplitDesign writes the new
// shape — the migration is the ordinary save. So does a design from before the
// file existed (liftLegacyDefinitions).

package spec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DependencyDesignFile is the definition's file name in its directory.
const DependencyDesignFile = "dependency.json"

// SdkManifestFile is the SDK manifest's file name in a dependency directory.
const SdkManifestFile = "sdk.json"

// dependencyDesignKey is the design-relative key of a dependency's file.
func dependencyDesignKey(name string) string {
	return dependencyDirPrefix + name + "/" + DependencyDesignFile
}

// ContractPath is the REPO-relative path of a dependency's committed contract
// — what the coding agent reads. Empty when the dependency has no contract.
func ContractPath(depName, contractFile string) string {
	if contractFile == "" {
		return ""
	}
	return DesignDir + "/" + dependencyDirPrefix + depName + "/" + contractFile
}

// StyleForContractType is the computed consumption style for a contract type:
// how the component talks to the system. Empty for a type no project contract
// has (asyncapi, protobuf, documentation exist only on a registry record).
func StyleForContractType(contractType string) DependencyStyle {
	switch contractType {
	case DependencyContractTypeOpenAPI:
		return DependencyStyleRestAPI
	case DependencyContractTypeGraphQL:
		return DependencyStyleGraphQL
	case DependencyContractTypeSDK:
		return DependencyStyleSDK
	}
	return ""
}

// ContractTypeForStyle is StyleForContractType's inverse, for lifting a flat
// (pre-resource) file whose only evidence of the contract's kind is its style.
func ContractTypeForStyle(style DependencyStyle) string {
	switch style {
	case DependencyStyleRestAPI:
		return DependencyContractTypeOpenAPI
	case DependencyStyleGraphQL:
		return DependencyContractTypeGraphQL
	case DependencyStyleSDK:
		return DependencyContractTypeSDK
	}
	return ""
}

// --- on-disk shapes -------------------------------------------------------

type dependencyDefinitionJSON struct {
	Name        string           `json:"name"`
	Resource    resourceJSON     `json:"resource"`
	Provenance  *provenanceJSON  `json:"provenance,omitempty"`
	Suggestions []suggestionJSON `json:"suggestions,omitempty"`
}

type resourceJSON struct {
	Ref                     string          `json:"ref,omitempty"`
	Name                    string          `json:"name"`
	Description             string          `json:"description,omitempty"`
	Provider                string          `json:"provider,omitempty"`
	Config                  []configKeyJSON `json:"config,omitempty"`
	Contract                *contractJSON   `json:"contract,omitempty"`
	ConsumptionInstructions string          `json:"consumptionInstructions,omitempty"`
	Provenance              *provenanceJSON `json:"provenance,omitempty"`
}

type contractJSON struct {
	Type     string          `json:"type"`
	Path     string          `json:"path"`
	Origin   string          `json:"origin,omitempty"`
	Accepted *assumptionJSON `json:"accepted,omitempty"`
}

type provenanceJSON struct {
	SourceURL string `json:"sourceUrl,omitempty"`
	Registry  string `json:"registry,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	ReadOn    string `json:"readOn,omitempty"`
}

// suggestionJSON is the on-disk shape of one entry in a dependency's
// `suggestions` array. Mirrors DependencySuggestion.
type suggestionJSON struct {
	Name        string `json:"name"`
	Style       string `json:"style,omitempty"`
	Description string `json:"description,omitempty"`
}

type assumptionJSON struct {
	By   string `json:"by"`
	At   string `json:"at"`
	Note string `json:"note,omitempty"`
}

type sdkManifestJSON struct {
	Packages map[string]string `json:"packages"`
	DocsURL  string            `json:"docsUrl,omitempty"`
	Calls    []string          `json:"calls,omitempty"`
	Derived  bool              `json:"derived,omitempty"`
	Assumed  bool              `json:"assumed,omitempty"`
}

// flatDefinitionJSON is the PREVIOUS file shape — provider, style, a contract
// file name, source and the acceptance record at the top level. Decoded only
// to lift; never encoded.
type flatDefinitionJSON struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Source      string              `json:"source,omitempty"`
	Provider    string              `json:"provider,omitempty"`
	Style       string              `json:"style,omitempty"`
	Contract    string              `json:"contract,omitempty"`
	SDK         string              `json:"sdk,omitempty"`
	Provenance  *flatProvenanceJSON `json:"provenance,omitempty"`
	Suggestions []suggestionJSON    `json:"suggestions,omitempty"`
	Candidates  []candidateJSON     `json:"candidates,omitempty"`
	Config      []configKeyJSON     `json:"config,omitempty"`
	Assumed     *assumptionJSON     `json:"assumed,omitempty"`
}

type flatProvenanceJSON struct {
	SourceURL string `json:"sourceUrl,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	FetchedAt string `json:"fetchedAt,omitempty"`
	Sliced    bool   `json:"sliced,omitempty"`
}

// --- decode / encode -------------------------------------------------------

// parseDependencyDefinitionJSON decodes one dependency.json. Strict on unknown
// keys (the agent's write-gate is; a file that got past it is a bug worth
// surfacing, not smoothing over) and on the name-equals-directory rule. A
// file in the previous flat shape is lifted into the nested one.
func parseDependencyDefinitionJSON(dir, raw string) (DependencyDefinition, error) {
	if !componentDesignName.MatchString(dir) {
		return DependencyDefinition{}, fmt.Errorf("dependency directory %q is not kebab-case", dir)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return DependencyDefinition{}, fmt.Errorf("decode %s: %w", DependencyDesignFile, err)
	}
	var def DependencyDefinition
	if _, nested := probe["resource"]; nested {
		dj, err := strictDecode[dependencyDefinitionJSON](raw)
		if err != nil {
			return DependencyDefinition{}, err
		}
		def = toModelDefinition(dj)
	} else {
		fj, err := strictDecode[flatDefinitionJSON](raw)
		if err != nil {
			return DependencyDefinition{}, err
		}
		def = liftFlatDefinition(fj)
	}
	if def.Name != dir {
		return DependencyDefinition{}, fmt.Errorf("%s name %q must equal the dependency directory %q", DependencyDesignFile, def.Name, dir)
	}
	if def.Resource.Name == "" {
		def.Resource.Name = def.Name
	}
	if def.Resource.Name != def.Name {
		return DependencyDefinition{}, fmt.Errorf("%s resource.name %q must equal the dependency name %q", DependencyDesignFile, def.Resource.Name, def.Name)
	}
	if def.Resource.Ref != "" && def.Resource.Ref != def.Name {
		return DependencyDefinition{}, fmt.Errorf("%s resource.ref %q must equal the dependency name %q — a registered resource is used under its own name", DependencyDesignFile, def.Resource.Ref, def.Name)
	}
	return def, nil
}

func strictDecode[T any](raw string) (T, error) {
	var out T
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, fmt.Errorf("decode %s: %w", DependencyDesignFile, err)
	}
	// More() answers "another element in the array or object being read", so a
	// stray closing delimiter slips past it. Asking for one more value and
	// requiring EOF is the whole-stream check.
	var rest json.RawMessage
	if err := dec.Decode(&rest); !errors.Is(err, io.EOF) {
		return out, fmt.Errorf("decode %s: unexpected trailing content", DependencyDesignFile)
	}
	return out, nil
}

func toModelDefinition(dj dependencyDefinitionJSON) DependencyDefinition {
	def := DependencyDefinition{
		Name:        dj.Name,
		Resource:    toModelResource(dj.Resource),
		Provenance:  toModelProvenance(dj.Provenance),
		Suggestions: toModelSuggestions(dj.Suggestions),
	}
	return def
}

func toModelResource(rj resourceJSON) ResourceDefinition {
	r := ResourceDefinition{
		Ref:                     rj.Ref,
		Name:                    rj.Name,
		Description:             rj.Description,
		Provider:                rj.Provider,
		Config:                  toModelConfigKeys(rj.Config),
		ConsumptionInstructions: rj.ConsumptionInstructions,
		Provenance:              toModelProvenance(rj.Provenance),
	}
	if rj.Contract != nil {
		r.Contract = &ResourceContract{Type: rj.Contract.Type, Path: rj.Contract.Path, Origin: rj.Contract.Origin}
		if rj.Contract.Accepted != nil {
			r.Contract.Accepted = &DependencyAssumption{By: rj.Contract.Accepted.By, At: rj.Contract.Accepted.At, Note: rj.Contract.Accepted.Note}
		}
	}
	return r
}

func toModelProvenance(pj *provenanceJSON) *ResourceProvenance {
	if pj == nil {
		return nil
	}
	return &ResourceProvenance{SourceURL: pj.SourceURL, Registry: pj.Registry, SHA256: pj.SHA256, ReadOn: pj.ReadOn}
}

// liftFlatDefinition turns the previous flat file shape into the nested one.
// The style names the contract type; the sdk manifest, when the style is sdk,
// IS the contract; `source: org` becomes a Ref; `assumed` becomes the
// contract's acceptance; `fetchedAt` becomes `readOn`; `sliced` is dropped (a
// contract is a whole document now — a slice on disk still reads, it is
// simply the file the project has). Origin is left empty: the file markers
// decide it at hydration, as they always did for these files.
func liftFlatDefinition(fj flatDefinitionJSON) DependencyDefinition {
	def := DependencyDefinition{
		Name: fj.Name,
		Resource: ResourceDefinition{
			Name:        fj.Name,
			Description: fj.Description,
			Provider:    fj.Provider,
			Config:      toModelConfigKeys(fj.Config),
		},
		Suggestions: append(toModelSuggestions(fj.Suggestions), toModelCandidates(fj.Candidates)...),
	}
	if fj.Source == DependencySourceOrg {
		def.Resource.Ref = fj.Name
	}
	path := fj.Contract
	if fj.Style == DependencyStyleSDK && fj.SDK != "" {
		path = fj.SDK
	}
	if path != "" {
		def.Resource.Contract = &ResourceContract{Type: ContractTypeForStyle(fj.Style), Path: path}
		if def.Resource.Contract.Type == "" {
			def.Resource.Contract.Type = contractTypeForFile(path)
		}
		if fj.Assumed != nil {
			def.Resource.Contract.Accepted = &DependencyAssumption{By: fj.Assumed.By, At: fj.Assumed.At, Note: fj.Assumed.Note}
		}
	}
	if fj.Provenance != nil {
		def.Provenance = &ResourceProvenance{SourceURL: fj.Provenance.SourceURL, SHA256: fj.Provenance.SHA256, ReadOn: fj.Provenance.FetchedAt}
	}
	return def
}

// contractTypeForFile names a contract type from a file name alone, for a
// lifted file that recorded no style.
func contractTypeForFile(file string) string {
	switch {
	case file == SdkManifestFile:
		return DependencyContractTypeSDK
	case strings.HasSuffix(file, ".graphql"), strings.HasSuffix(file, ".graphqls"):
		return DependencyContractTypeGraphQL
	default:
		return DependencyContractTypeOpenAPI
	}
}

func marshalDependencyDefinitionJSON(dir string, def DependencyDefinition) ([]byte, error) {
	if def.Name == "" {
		return nil, fmt.Errorf("dependency with empty name")
	}
	if !componentDesignName.MatchString(def.Name) {
		return nil, fmt.Errorf("dependency name %q is not kebab-case", def.Name)
	}
	if def.Name != dir {
		return nil, fmt.Errorf("dependency name %q must equal the dependency directory %q", def.Name, dir)
	}
	if def.Resource.Ref != "" && def.Resource.Ref != def.Name {
		return nil, fmt.Errorf("dependency %q: resource.ref %q must equal the dependency name", def.Name, def.Resource.Ref)
	}
	res := def.Resource
	if res.Name == "" {
		res.Name = def.Name
	}
	dj := dependencyDefinitionJSON{
		Name:        def.Name,
		Resource:    toJSONResource(res),
		Provenance:  toJSONProvenance(def.Provenance),
		Suggestions: toJSONSuggestions(def.Suggestions),
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(dj); err != nil {
		return nil, fmt.Errorf("encode %s: %w", DependencyDesignFile, err)
	}
	return buf.Bytes(), nil
}

func toJSONResource(r ResourceDefinition) resourceJSON {
	rj := resourceJSON{
		Ref:                     r.Ref,
		Name:                    r.Name,
		Description:             r.Description,
		Provider:                r.Provider,
		Config:                  toJSONConfigKeys(r.Config),
		ConsumptionInstructions: r.ConsumptionInstructions,
		Provenance:              toJSONProvenance(r.Provenance),
	}
	if r.Contract != nil {
		rj.Contract = &contractJSON{Type: r.Contract.Type, Path: r.Contract.Path, Origin: r.Contract.Origin}
		if r.Contract.Accepted != nil {
			rj.Contract.Accepted = &assumptionJSON{By: r.Contract.Accepted.By, At: r.Contract.Accepted.At, Note: r.Contract.Accepted.Note}
		}
	}
	return rj
}

func toJSONProvenance(p *ResourceProvenance) *provenanceJSON {
	if p == nil {
		return nil
	}
	return &provenanceJSON{SourceURL: p.SourceURL, Registry: p.Registry, SHA256: p.SHA256, ReadOn: p.ReadOn}
}

// parseSdkManifestJSON decodes one sdk.json.
func parseSdkManifestJSON(raw string) (SdkManifest, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var mj sdkManifestJSON
	if err := dec.Decode(&mj); err != nil {
		return SdkManifest{}, fmt.Errorf("decode %s: %w", SdkManifestFile, err)
	}
	return SdkManifest{Packages: mj.Packages, DocsURL: mj.DocsURL, Calls: mj.Calls, Derived: mj.Derived, Assumed: mj.Assumed}, nil
}

// assembleDependencyDefinitions parses every dependency directory in the
// design file map, sorted by name. A directory without a dependency.json is
// skipped — a contract uploaded before its definition is not a dependency yet.
func assembleDependencyDefinitions(files map[string]string) ([]DependencyDefinition, error) {
	names := DependencyNamesIn(files)
	out := make([]DependencyDefinition, 0, len(names))
	for _, name := range names {
		raw, ok := files[dependencyDesignKey(name)]
		if !ok {
			continue
		}
		def, err := parseDependencyDefinitionJSON(name, raw)
		if err != nil {
			return nil, fmt.Errorf("assemble dependency %q: %w", name, err)
		}
		out = append(out, def)
	}
	return out, nil
}

// liftLegacyDefinitions adds, for every external edge that has no definition
// on disk but still carries one on the component (a design from before the
// dependency file existed), a definition built from what the components say —
// config keys unioned across the consumers that carry them, the first
// non-empty style / suggestions / contract URL winning. A definition already on
// disk is never touched: the file is the truth, and a stale copy on a
// component is exactly what the next save strips. The result is sorted by
// name; the second return is the components that carried legacy fields, so the
// save that writes the lifted files also re-renders them. A reference with no
// definition anywhere is left alone: hydration reads it as needs-input, which
// is the truth.
func liftLegacyDefinitions(defs []DependencyDefinition, comps []DesignComponent) ([]DependencyDefinition, []string) {
	onDisk := make(map[string]bool, len(defs))
	for _, d := range defs {
		onDisk[d.Name] = true
	}
	lifted := map[string]int{}
	var carriers []string
	for _, c := range comps {
		carried := false
		for _, d := range c.Dependencies {
			if d.Kind != DependencyKindExternal || d.Name == "" {
				continue
			}
			hasLegacy := d.Style != "" || d.Package != "" || len(d.Suggestions) > 0 || len(d.Config) > 0 || d.Provenance != nil
			if !hasLegacy {
				continue
			}
			carried = true
			if onDisk[d.Name] {
				continue
			}
			if i, ok := lifted[d.Name]; ok {
				defs[i].Resource.Config = unionConfigKeys(defs[i].Resource.Config, d.Config)
				continue
			}
			// The legacy shape had no provider name — the style is the only
			// evidence a system was chosen, and nothing here invents one — and
			// the coding agent's research pointer (the old specPath) is
			// provenance, not a contract.
			def := DependencyDefinition{Name: d.Name, Resource: ResourceDefinition{Name: d.Name, Description: d.Description}}
			def.Suggestions = append([]DependencySuggestion(nil), d.Suggestions...)
			def.Resource.Config = append([]ConfigKey(nil), d.Config...)
			if d.Provenance != nil {
				p := *d.Provenance
				def.Provenance = &p
			}
			lifted[d.Name] = len(defs)
			defs = append(defs, def)
		}
		if carried {
			carriers = append(carriers, c.Name)
		}
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs, carriers
}

// unionConfigKeys merges two key lists by key name; a secret marking wins on
// conflict, the first description/default is kept. Order: a's, then b's new.
func unionConfigKeys(a, b []ConfigKey) []ConfigKey {
	index := make(map[string]int, len(a))
	out := append([]ConfigKey(nil), a...)
	for i, k := range out {
		index[k.Key] = i
	}
	for _, k := range b {
		if i, ok := index[k.Key]; ok {
			if k.Secret {
				out[i].Secret = true
				out[i].DefaultValue = ""
			}
			if out[i].Description == "" {
				out[i].Description = k.Description
			}
			continue
		}
		index[k.Key] = len(out)
		out = append(out, k)
	}
	return out
}

// hydrateExternalDependencies copies each definition onto every component edge
// that references it, and reads the directory for what the definition points
// at: the contract file's presence, and — for an sdk contract — the manifest's
// package for the component's language. Style, ContractAssumed and
// ContractDerived are COMPUTED here from the contract's type and origin (with
// the file markers as a fallback for a file that recorded no origin). An edge
// with no definition keeps only what the component said (kind, name,
// description, wiring) and reads as needs-input.
func hydrateExternalDependencies(d *DesignFile, files map[string]string) {
	defs := make(map[string]DependencyDefinition, len(d.Dependencies))
	for _, def := range d.Dependencies {
		defs[def.Name] = def
	}
	manifests := make(map[string]SdkManifest)
	for i := range d.Components {
		comp := &d.Components[i]
		for j := range comp.Dependencies {
			dep := &comp.Dependencies[j]
			if dep.Kind != DependencyKindExternal {
				continue
			}
			def, ok := defs[dep.Name]
			if !ok {
				// Nothing on disk and nothing lifted: strip any legacy carry so
				// the edge reads as the reference it is.
				dep.Style, dep.Package, dep.Suggestions, dep.Config, dep.Provenance = "", "", nil, nil, nil
				continue
			}
			res := def.Resource
			dep.ResourceRef = res.Ref
			dep.Source = DependencySourceProject
			if res.Ref != "" {
				dep.Source = DependencySourceOrg
			}
			dep.Provider = res.Provider
			dep.ConsumptionInstructions = res.ConsumptionInstructions
			dep.Suggestions = append([]DependencySuggestion(nil), def.Suggestions...)
			dep.Config = append([]ConfigKey(nil), res.Config...)
			dep.Provenance = nil
			if def.Provenance != nil {
				p := *def.Provenance
				dep.Provenance = &p
			}
			if dep.Description == "" {
				dep.Description = res.Description
			}
			// The contract counts only when the file is actually beside the
			// definition — a path pointing at nothing is no contract.
			dep.Contract, dep.SDK, dep.Package = "", "", ""
			dep.ContractType, dep.ContractOrigin, dep.Style = "", "", ""
			dep.ContractAssumed, dep.ContractDerived, dep.Assumed = false, false, nil
			if res.Contract == nil {
				continue
			}
			dep.ContractType = res.Contract.Type
			dep.ContractOrigin = res.Contract.Origin
			dep.Style = StyleForContractType(res.Contract.Type)
			if res.Contract.Accepted != nil {
				a := *res.Contract.Accepted
				dep.Assumed = &a
			}
			raw, present := files[dependencyDirPrefix+dep.Name+"/"+res.Contract.Path]
			if !present {
				continue
			}
			switch res.Contract.Origin {
			case DependencyContractOriginAssumed:
				dep.ContractAssumed = true
			case DependencyContractOriginDerived:
				dep.ContractDerived = true
			case "":
				// A file that predates origin says it in its own body.
				dep.ContractAssumed, dep.ContractDerived = contractMarkers(raw)
			}
			if res.Contract.Type == DependencyContractTypeSDK {
				key := dependencyDirPrefix + dep.Name + "/" + res.Contract.Path
				m, cached := manifests[key]
				if !cached {
					if parsed, err := parseSdkManifestJSON(raw); err == nil {
						m = parsed
					}
					manifests[key] = m
				}
				// Having its SDK means having a package the component can
				// install. A manifest that names packages for other languages
				// only is not this component's contract: setting SDK from it
				// would read resolved while the coding agent has nothing to
				// add to its manifest. A component with no language yet has
				// nothing to select against, so the manifest still counts.
				language := strings.ToLower(strings.TrimSpace(comp.Language))
				pkg, named := m.Packages[language], false
				if language != "" {
					_, named = m.Packages[language]
				} else {
					named = len(m.Packages) > 0
				}
				if !named {
					continue
				}
				dep.SDK = res.Contract.Path
				dep.Package = pkg
				if res.Contract.Origin == "" {
					dep.ContractAssumed, dep.ContractDerived = m.Assumed, m.Derived
				}
				continue
			}
			dep.Contract = res.Contract.Path
		}
	}
}

// contractTitle is the system an OpenAPI document (YAML or JSON) names in
// `info.title`, or "" when it names none. Handing over a document is choosing
// its system (CollectDependencyContract writes this as the provider); nothing
// is derived from it at read time — a file with no provider reads as unchosen,
// on every surface alike, until the user's choice is written to it.
func contractTitle(raw string) string {
	var doc struct {
		Info struct {
			Title string `yaml:"title"`
		} `yaml:"info"`
	}
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return ""
	}
	return strings.TrimSpace(doc.Info.Title)
}

// contractMarkers reports what a contract file written before `origin`
// existed declares itself to be: an OpenAPI document (YAML or JSON) with
// `x-aep-assumed: true` or `x-aep-derived: true` at the root, or a GraphQL
// schema carrying the same as a `# …: true` comment line. Read only as the
// fallback for a contract with no recorded origin.
func contractMarkers(raw string) (assumed, derived bool) {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(raw), &doc); err == nil && doc != nil {
		if v, ok := doc["x-aep-assumed"].(bool); ok {
			assumed = v
		}
		if v, ok := doc["x-aep-derived"].(bool); ok {
			derived = v
		}
	}
	for _, line := range strings.Split(raw, "\n") {
		switch strings.TrimSpace(line) {
		case "# x-aep-assumed: true":
			assumed = true
		case "# x-aep-derived: true":
			derived = true
		}
	}
	return assumed, derived
}

// contractMarkedAssumed is contractMarkers' assumed half, for the callers
// that only ask that.
func contractMarkedAssumed(raw string) bool {
	assumed, _ := contractMarkers(raw)
	return assumed
}
