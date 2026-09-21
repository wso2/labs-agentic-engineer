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
	"log/slog"
	"sort"
	"time"
)

// ErrDependencyIsCopy: the dependency already holds a copy of an organization
// record (`resource.ref` is set), so there is nothing of the project's own to
// promote.
var ErrDependencyIsCopy = errors.New("dependency already reuses an organization record")

// ProjectResource is a project's own external resource as the organization
// reads it to take it over: the dependency file's definition and the contract
// document beside it (empty when the definition names none, or the file is not
// on disk yet).
type ProjectResource struct {
	Definition DependencyDefinition
	Document   string
}

// ReadProjectResource returns the project's own resource named depName. A
// dependency that is already a copy (ref set) is refused with
// ErrDependencyIsCopy: the organization holds that record already.
func (s *designService) ReadProjectResource(ctx context.Context, orgID, projectID, depName string) (*ProjectResource, error) {
	design, err := s.store.ReadDesign(ctx, orgID, projectID)
	if err != nil {
		if IsNotFound(err) {
			return nil, fmt.Errorf("%w: no design for project %q", ErrDependencyNotFound, projectID)
		}
		return nil, fmt.Errorf("read design: %w", err)
	}
	if design == nil {
		return nil, fmt.Errorf("%w: no design for project %q", ErrDependencyNotFound, projectID)
	}
	def, found := definitionByName(design.Dependencies, depName)
	if !found {
		return nil, fmt.Errorf("%w: dependency %q has no definition", ErrDependencyNotFound, depName)
	}
	if def.Resource.Ref != "" {
		return nil, fmt.Errorf("%w: %q", ErrDependencyIsCopy, depName)
	}
	out := &ProjectResource{Definition: def}
	if c := def.Resource.Contract; c != nil && c.Path != "" && s.fileCommitter != nil {
		raw, _, exists, rerr := s.fileCommitter.ReadFile(ctx, orgID, projectID, ContractPath(depName, c.Path))
		if rerr != nil {
			return nil, fmt.Errorf("read contract for %q: %w", depName, rerr)
		}
		if exists {
			out.Document = raw
		}
	}
	return out, nil
}

// RewriteAsRegistryCopy replaces the project's own resource with a copy of the
// organization's record, exactly as a fresh reuse would land it: the same
// renderer the platform runs when a design turn names a registered resource
// (`resource.ref`, the record's block and instructions, `contract.origin:
// registry`, provenance naming the registry document and its hash). The
// document is rewritten with the record's bytes so the two never disagree.
func (s *designService) RewriteAsRegistryCopy(ctx context.Context, orgID, projectID, depName string, rec RegisteredResource) error {
	if s.fileCommitter == nil {
		return fmt.Errorf("promotion unavailable: no committed-truth write surface wired")
	}
	stub := DependencyDefinition{Name: depName, Resource: ResourceDefinition{Ref: depName, Name: depName}}
	body, files, err := renderRegistryCopy(stub, rec, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("render dependency %q as a registry copy: %w", depName, err)
	}
	definitionFull := DesignDir + "/" + dependencyDesignKey(depName)
	_, sha, exists, err := s.fileCommitter.ReadFile(ctx, orgID, projectID, definitionFull)
	if err != nil {
		return fmt.Errorf("read dependency.json for CAS: %w", err)
	}
	if !exists {
		return fmt.Errorf("%w: dependency %q file missing on disk", ErrDependencyNotFound, depName)
	}
	writes := []DesignFileWrite{{Path: definitionFull, Content: body, BaseSHA: sha}}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		_, fsha, _, rerr := s.fileCommitter.ReadFile(ctx, orgID, projectID, p)
		if rerr != nil {
			return fmt.Errorf("read %s for CAS: %w", p, rerr)
		}
		writes = append(writes, DesignFileWrite{Path: p, Content: files[p], BaseSHA: fsha})
	}
	if err := s.fileCommitter.Commit(ctx, orgID, projectID, writes,
		fmt.Sprintf("Promote dependency %s to the organization's registry", depName)); err != nil {
		return err
	}
	slog.InfoContext(ctx, "dependency promoted to the registry", "org", orgID, "project", projectID, "dependency", depName)
	return nil
}
