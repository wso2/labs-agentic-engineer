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

package requirements

import (
	"github.com/wso2/asdlc/asdlc-service/internal/feature/artifacts"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// Tag scheme:
//   - Requirements: `v<N>` (versioned independently)
//
// The BFF's models.ArtifactVersion is a flat shape used by the UI.

// mapRequirementsVersions converts the artifact-service requirements version
// list to the BFF's flat ArtifactVersion shape.
func mapRequirementsVersions(versions []artifacts.RequirementsVersionInfo) []models.ArtifactVersion {
	if len(versions) == 0 {
		return nil
	}
	out := make([]models.ArtifactVersion, 0, len(versions))
	for _, v := range versions {
		out = append(out, models.ArtifactVersion{
			Version:    v.Version,
			TagName:    v.Tag,
			CommitHash: v.CommitHash,
		})
	}
	return out
}
