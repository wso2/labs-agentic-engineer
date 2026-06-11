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

package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/wso2/asdlc/asdlc-service/models"
)

// ErrComponentRemovedAfterGeneration is returned when a task references a
// component that no longer exists in the project's `specs/design/` tree.
// See docs/design/tech-lead-agent.md §10.4 — reconciliation auto-closes
// pending tasks for removed components on every design save, so this case
// should be rare. When it does happen, the dispatch / issue-body builder
// fails fast rather than rendering placeholders.
var ErrComponentRemovedAfterGeneration = errors.New("component removed after generation")

// resolveDesignComponent reads the project's current `specs/design/` tree
// and returns the entry whose Name matches task.ComponentName. Per design
// §12, dispatch reads the *current* design at dispatch time — not a snapshot
// from when the task was generated — so design edits between generation and
// dispatch propagate.
//
// Lookups are case-insensitive on Name to mirror toposort/lookup behaviour
// elsewhere in the codebase.
func (s *taskService) resolveDesignComponent(ctx context.Context, task *models.ComponentTask) (*models.DesignComponent, error) {
	return resolveDesignComponentVia(ctx, s.store, task)
}

func resolveDesignComponentVia(ctx context.Context, store *ArtifactStore, task *models.ComponentTask) (*models.DesignComponent, error) {
	if store == nil {
		return nil, fmt.Errorf("artifact store not configured")
	}
	design, err := store.ReadDesign(ctx, task.OrgID, task.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("read design for task %s: %w", task.ID, err)
	}
	if design == nil {
		return nil, fmt.Errorf("design missing for project %s (no specs/design/design.md)", task.ProjectID)
	}
	target := strings.ToLower(task.ComponentName)
	for i := range design.Components {
		if strings.EqualFold(design.Components[i].Name, target) {
			c := design.Components[i]
			return &c, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrComponentRemovedAfterGeneration, task.ComponentName)
}
