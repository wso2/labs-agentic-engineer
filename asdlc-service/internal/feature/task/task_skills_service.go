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

package task

import (
	"context"
	"fmt"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/artifacts"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// TaskSkillEntry is one row in the runner's pull response. Mirrors
// `SkillResolution` in the remote-worker.
type TaskSkillEntry struct {
	ID               string            `json:"id"`               // e.g. "builtin/api-management"
	MaterializedName string            `json:"materializedName"` // e.g. "builtin-api-management"
	Kind             string            `json:"kind"`             // builtin | custom | imported
	SkillMD          string            `json:"skillMd"`
	References       map[string]string `json:"references"`
}

// TaskSkillsResponse is the pull endpoint's wire body.
type TaskSkillsResponse struct {
	Skills []TaskSkillEntry `json:"skills"`
}

// TaskSkillsService backs GET /api/v1/tasks/:taskId/skills. With the repo as
// the single source of truth and no snapshots, it reads the task's design
// `skillsApplied` and resolves each name against the org's skills repo at HEAD
// (docs/design/skills-repo-storage.md §7.4) — mid-flight drift is accepted.
type TaskSkillsService struct {
	taskRepo interface {
		GetByID(ctx context.Context, id string) (*models.ComponentTask, error)
	}
	store    *artifacts.ArtifactStore
	skillSvc SkillResolver
}

func NewTaskSkillsService(
	taskRepo interface {
		GetByID(ctx context.Context, id string) (*models.ComponentTask, error)
	},
	store *artifacts.ArtifactStore,
	skillSvc SkillResolver,
) *TaskSkillsService {
	return &TaskSkillsService{taskRepo: taskRepo, store: store, skillSvc: skillSvc}
}

// SkillsForTask resolves the task → reads the design's attached skills →
// resolves them against the repo at HEAD → returns the wire response. Returns
// ErrTaskNotFound when the task doesn't exist, or an empty Skills list (NOT an
// error) when the design has no attached skills.
func (s *TaskSkillsService) SkillsForTask(ctx context.Context, taskID string) (*TaskSkillsResponse, error) {
	if s == nil || s.taskRepo == nil {
		return nil, fmt.Errorf("task skills service: not configured")
	}
	empty := &TaskSkillsResponse{Skills: []TaskSkillEntry{}}

	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}
	if task == nil {
		return nil, ErrTaskNotFound
	}
	if s.store == nil || s.skillSvc == nil {
		return empty, nil
	}

	design, err := s.store.ReadDesign(ctx, task.OrgID, task.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("read design: %w", err)
	}
	if design == nil || len(design.SkillsApplied) == 0 {
		return empty, nil
	}

	resolved, err := s.skillSvc.ResolveMany(ctx, task.OrgID, design.SkillsApplied)
	if err != nil {
		return nil, fmt.Errorf("resolve skills: %w", err)
	}
	if len(resolved) == 0 {
		return empty, nil
	}

	out := make([]TaskSkillEntry, 0, len(resolved))
	for _, sk := range resolved {
		out = append(out, TaskSkillEntry{
			ID:               models.PrefixedID(sk.Kind, sk.Name),
			MaterializedName: models.MaterializedName(sk.Kind, sk.Name),
			Kind:             sk.Kind,
			SkillMD:          sk.SkillMD,
			References:       sk.References,
		})
	}
	return &TaskSkillsResponse{Skills: out}, nil
}
