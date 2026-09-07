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

package spec

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrComponentRemovedAfterGeneration is returned when a task references a
// component that no longer exists in the project's `specs/design/` tree.
// See docs/design/tech-lead-agent.md §10.4 — reconciliation auto-closes
// pending tasks for removed components on every design save, so this case
// should be rare. When it does happen, the dispatch / issue-body builder
// fails fast rather than rendering placeholders.
var ErrComponentRemovedAfterGeneration = errors.New("component removed after generation")

// ResolveDesignComponent reads the project's current `specs/design/` tree via
// the given store and returns the entry whose Name matches componentName. The
// design at HEAD is read at the moment of use (not a snapshot), so design edits
// propagate. Lookups are case-insensitive on Name.
func ResolveDesignComponent(ctx context.Context, store *ArtifactStore, orgID, projectID, componentName string) (*DesignComponent, error) {
	if store == nil {
		return nil, fmt.Errorf("artifact store not configured")
	}
	design, err := store.ReadDesign(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("read design for %s/%s: %w", orgID, projectID, err)
	}
	if design == nil {
		return nil, fmt.Errorf("design missing for project %s (no specs/design/design.cell)", projectID)
	}
	for i := range design.Components {
		if strings.EqualFold(design.Components[i].Name, componentName) {
			c := design.Components[i]
			return &c, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrComponentRemovedAfterGeneration, componentName)
}
