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

package app

import (
	"context"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/securityspec"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// securityJSONReader reads the project's security.json from the design bundle
// for platform-resource provision overlay. An empty tag is HEAD (the HTTP
// drawer, which has no version); a build supplies the version tag it is
// building, so the overlay is what THAT version declares rather than whatever
// has been edited since.
type securityJSONReader struct{ art spec.ArtifactService }

func (r securityJSONReader) ReadSecurityJSON(ctx context.Context, orgID, projectID, tag string) ([]byte, error) {
	var (
		files map[string]string
		err   error
	)
	if tag == "" {
		files, err = r.art.ListDesignFiles(ctx, orgID, projectID)
	} else {
		files, err = r.art.GetDesignAtTag(ctx, orgID, projectID, tag)
	}
	if err != nil {
		return nil, err
	}
	raw, ok := files[securityspec.BundleKey]
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	return []byte(raw), nil
}
