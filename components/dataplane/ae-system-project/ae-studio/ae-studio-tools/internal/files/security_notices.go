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

// Moved from services/aep-api/internal/spec/files_service.go
// (securityDesignNotices) and build_gate.go (buildGateWarnings); the aep-api
// copies are deleted in phase 4.

package files

import (
	"strings"

	"github.com/wso2/aep/ae-studio-tools/internal/securityspec"
)

// designPrefix is the repo-relative design directory (trailing slash so a
// prefix match never straddles a sibling like `specs/designs/`).
const designPrefix = "specs/design/"

// allowedDesignExts are the file extensions the design bundle holds:
// markdown, YAML (OpenAPI), JSON (design.json, security.json, projections),
// the design.cell DSL and the wireframes DSL.
var allowedDesignExts = []string{".md", ".yaml", ".yml", ".json", ".cell", ".dsl"}

// Warning codes of the security design's coverage notices. They name a
// channel entry; the securityspec message key names a sentence.
const (
	// codeSecurityHandleUsedNowhere: a catalog handle no operation and no
	// screen requires.
	codeSecurityHandleUsedNowhere = "SECURITY_HANDLE_USED_NOWHERE"
	// codeSecurityHandleUnreachable: a handle an operation requires that no
	// role grants.
	codeSecurityHandleUnreachable = "SECURITY_HANDLE_UNREACHABLE"
	// codeSecurityAssignToDirectoryChecked: INFO, a role delegates to a group
	// the org directory already holds, so the platform creates nothing.
	codeSecurityAssignToDirectoryChecked = "SECURITY_ASSIGN_TO_DIRECTORY_CHECKED"
	// codeSecurityDesignNotice: the fallback for a non-blocking finding with
	// no code of its own yet, so a new rule still reaches the channel.
	codeSecurityDesignNotice = "SECURITY_DESIGN_NOTICE"
)

// securityNoticeCodes maps a securityspec message key to the Warning code
// that carries it.
var securityNoticeCodes = map[string]string{
	securityspec.MsgHandleUsedNowhere:        codeSecurityHandleUsedNowhere,
	securityspec.MsgHandleUnreachable:        codeSecurityHandleUnreachable,
	securityspec.MsgAssignToDirectoryChecked: codeSecurityAssignToDirectoryChecked,
}

// treeReader is the one thing securityDesignNotices needs of the committed
// base tree: the content of a path it already knows exists.
type treeReader interface {
	Read(rel string) ([]byte, string, error)
}

// securityDesignNotices are the security design's non-blocking coverage
// notices for the tree this apply LANDS: "declared, used nowhere",
// "unreachable by any role", and the INFO note that an assignTo group is one
// the directory already holds. A coverage fact is a statement about
// security.json read against design.cell, the wireframes and the component
// specs together, and any of those may be untouched by this batch, so the
// committed tree is read for the ones the batch does not carry.
//
// It costs those reads, so it runs only when the batch touches specs/design/
// AND the landed tree has a security.json.
func securityDesignNotices(base treeReader, current, batch map[string]string, deleted map[string]bool) []Warning {
	touchesDesign := false
	for path := range batch {
		if strings.HasPrefix(path, designPrefix) {
			touchesDesign = true
			break
		}
	}
	if !touchesDesign {
		return nil
	}
	if _, inBatch := batch[securityspec.Path]; !inBatch {
		if _, inTree := current[securityspec.Path]; !inTree || deleted[securityspec.Path] {
			return nil
		}
	}

	bundle := map[string]string{}
	for path := range current {
		rel, ok := strings.CutPrefix(path, designPrefix)
		if !ok || rel == "" || !hasAllowedDesignExt(rel) || deleted[path] {
			continue
		}
		if _, inBatch := batch[path]; inBatch {
			continue // the batch's version is the one that lands
		}
		content, _, err := base.Read(path)
		if err != nil {
			continue // unreadable is the same as absent: the rule that needs it is skipped
		}
		bundle[rel] = string(content)
	}
	for path, content := range batch {
		rel, ok := strings.CutPrefix(path, designPrefix)
		if !ok || rel == "" || !hasAllowedDesignExt(rel) {
			continue
		}
		bundle[rel] = content
	}

	// coverageWarnings speaks bundle-relative paths; this channel is keyed by
	// repo path, the same as every other Warning the apply returns.
	notices := coverageWarnings(bundle)
	for i := range notices {
		notices[i].Path = designPrefix + notices[i].Path
	}
	return notices
}

// coverageWarnings are the security design's NON-BLOCKING findings over a
// design bundle (keys relative to specs/design/). Errors are the build
// gate's business and are excluded here: one defect, one message. A
// security.json that does not parse yields nothing (softValidate reports it).
func coverageWarnings(designFiles map[string]string) []Warning {
	raw, present := designFiles[securityspec.BundleKey]
	if !present || strings.TrimSpace(raw) == "" {
		return nil
	}
	doc, err := securityspec.Parse([]byte(raw))
	if err != nil {
		return nil
	}
	var out []Warning
	for _, finding := range securityspec.ReferenceFindings(doc, securityspec.DesignBundle(designFiles)) {
		if finding.Severity == securityspec.SeverityError {
			continue
		}
		code, ok := securityNoticeCodes[finding.Key]
		if !ok {
			code = codeSecurityDesignNotice
		}
		out = append(out, Warning{Path: securityspec.BundleKey, Code: code, Message: finding.Message})
	}
	return out
}

func hasAllowedDesignExt(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range allowedDesignExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}
