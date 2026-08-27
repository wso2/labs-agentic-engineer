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

package provisioning

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
)

// resourceDocWrite is one validated write row. File rows carry FileName+Content
// and are not committed until commitResourceDocs (after uniqueness / identity
// and remaining request checks). URL and keep-path rows never mint.
type resourceDocWrite struct {
	Type     string
	URL      string
	Path     string
	FileName string
	Content  string
}

// validateResourceDocWrites is the pure write-row validator. It never calls
// CommitUTF8. Every row is checked before any row is eligible to commit.
func validateResourceDocWrites(in []gen.ResourceDocWriteDTO) ([]resourceDocWrite, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]resourceDocWrite, 0, len(in))
	for i, d := range in {
		w, err := validateResourceDocWrite(i, d)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, nil
}

func validateResourceDocWrite(i int, d gen.ResourceDocWriteDTO) (resourceDocWrite, error) {
	if !d.Type.Valid() {
		return resourceDocWrite{}, apierr.BadRequest(fmt.Sprintf("resourceDocs[%d]: unknown type %q", i, d.Type))
	}

	u := strings.TrimSpace(d.URL)
	path := strings.TrimSpace(d.Path)
	fileName := strings.TrimSpace(d.FileName)
	content := d.Content
	fileNameSet := fileName != ""
	contentSet := content != ""
	if fileNameSet != contentSet {
		return resourceDocWrite{}, apierr.BadRequest(fmt.Sprintf("resourceDocs[%d]: fileName and content must both be provided", i))
	}

	n := 0
	if u != "" {
		n++
	}
	if path != "" {
		n++
	}
	if fileNameSet {
		n++
	}
	if n != 1 {
		return resourceDocWrite{}, apierr.BadRequest(fmt.Sprintf("resourceDocs[%d]: exactly one of url, path, or fileName+content is required", i))
	}

	switch {
	case u != "":
		parsed, perr := url.Parse(u)
		if perr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return resourceDocWrite{}, apierr.BadRequest(fmt.Sprintf("resourceDocs[%d]: url must be a valid http or https URL", i))
		}
		return resourceDocWrite{Type: string(d.Type), URL: u}, nil
	case path != "":
		if strings.Contains(path, "..") || strings.Contains(path, "\\") {
			return resourceDocWrite{}, apierr.BadRequest(fmt.Sprintf("resourceDocs[%d]: path must not contain .. or backslash", i))
		}
		return resourceDocWrite{Type: string(d.Type), Path: path}, nil
	default:
		if strings.ContainsAny(fileName, `/\`) || strings.Contains(fileName, "..") {
			return resourceDocWrite{}, apierr.BadRequest(fmt.Sprintf("resourceDocs[%d]: fileName must be a single path segment", i))
		}
		if !utf8.ValidString(content) {
			return resourceDocWrite{}, apierr.BadRequest(fmt.Sprintf("resourceDocs[%d]: content must be valid UTF-8", i))
		}
		return resourceDocWrite{Type: string(d.Type), FileName: fileName, Content: content}, nil
	}
}

// commitResourceDocs writes file rows via CommitUTF8 and returns catalog
// pointers. URL/path rows do not mint. Called in the same phase as
// OrgSecretWriter, after request checks succeed and before Ensure/Update.
func (s *Service) commitResourceDocs(ctx context.Context, orgID, logicalName string, writes []resourceDocWrite) ([]openchoreo.ResourceDoc, error) {
	if len(writes) == 0 {
		return nil, nil
	}
	hasFile := false
	for _, w := range writes {
		if w.FileName != "" {
			hasFile = true
			break
		}
	}
	if hasFile && s.orgResourceDocs == nil {
		return nil, fmt.Errorf("provisioning: org resource docs store is not configured")
	}

	out := make([]openchoreo.ResourceDoc, 0, len(writes))
	for _, w := range writes {
		switch {
		case w.FileName != "":
			path, err := s.orgResourceDocs.CommitUTF8(ctx, orgID, logicalName, w.FileName, w.Content)
			if err != nil {
				return nil, fmt.Errorf("provisioning: commit resource doc %q: %w", w.FileName, err)
			}
			out = append(out, openchoreo.ResourceDoc{Type: w.Type, Path: path})
		case w.URL != "":
			out = append(out, openchoreo.ResourceDoc{Type: w.Type, URL: w.URL})
		default:
			out = append(out, openchoreo.ResourceDoc{Type: w.Type, Path: w.Path})
		}
	}
	return out, nil
}
