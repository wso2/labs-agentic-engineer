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

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/securityspec"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// projectDisplayNamer resolves a project's display name for the end-user-auth
// overlay, which uses it as the sign-in client's name. It reads OpenChoreo
// directly rather than through the projects domain: the one fact needed is an
// annotation the project client already returns, and routing a label lookup
// through another domain's service would give provisioning a dependency on it.
type projectDisplayNamer struct {
	client interface {
		GetProject(ctx context.Context, orgName, projectName string) (*gen.Project, error)
	}
}

func (n projectDisplayNamer) ProjectDisplayName(ctx context.Context, orgID, projectID string) (string, error) {
	project, err := n.client.GetProject(ctx, orgID, projectID)
	if err != nil {
		return "", err
	}
	if project == nil {
		return "", nil
	}
	return project.DisplayName, nil
}

// securityJSONReader reads the project's security.json from the design bundle
// for platform-resource provision overlay. Empty tag is HEAD (HTTP drawer);
// a build's `v<N>` spec tag uses GetDesignAtSpecTag. GetDesignAtTag next door
// parses `v<N>-<M>` design-revision tags and refuses a spec tag — identity
// already works around this; this adapter does the same.
type securityJSONReader struct{ art spec.ArtifactService }

func (r securityJSONReader) ReadSecurityJSON(ctx context.Context, orgID, projectID, tag string) ([]byte, error) {
	var (
		files map[string]string
		err   error
	)
	if tag == "" {
		files, err = r.art.ListDesignFiles(ctx, orgID, projectID)
	} else {
		files, err = r.art.GetDesignAtSpecTag(ctx, orgID, projectID, tag)
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
