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

package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/wso2/aep/aep-api/internal/app/crtcatalog"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// refusedError is the derivation saying no to the design (an unknown
// resourceType, an auth conflict): the case POST /build refuses a build for.
// It is told apart from an I/O failure so the exit status can say which.
type refusedError struct{ err error }

func (e refusedError) Error() string { return e.err.Error() }
func (e refusedError) Unwrap() error { return e.err }

// deriveDir derives the design under designDir in place and returns the paths
// (relative to designDir) of the files it rewrote. A directory with no design
// root is a no-op, as it is for production.
func deriveDir(ctx context.Context, designDir, projectID, typesDir string) ([]string, error) {
	files, err := readDesignFiles(designDir)
	if err != nil {
		return nil, err
	}
	// The production read path, with no live resolvers wired: the statuses they
	// would add are read-time values that neither the derivation nor the render
	// looks at.
	design, err := spec.NewArtifactStore(nil).AssembleDesignFrom(ctx, projectID, files)
	if err != nil {
		return nil, fmt.Errorf("read design: %w", err)
	}
	if design == nil {
		return nil, nil
	}
	types, err := crtcatalog.New(
		dependencies.NewResourceTypeCatalog(openchoreo.ResourceTypeDir(typesDir), true),
	).ResourceTypesByName(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", spec.ErrResourceCatalogUnavailable, err)
	}
	derived, err := spec.DerivePlatformResourceFacts(design, types, projectID)
	if err != nil {
		if errors.Is(err, spec.ErrUnknownResourceType) || errors.Is(err, spec.ErrEndUserAuthConflict) {
			return nil, refusedError{err}
		}
		return nil, err
	}
	return writeDerived(designDir, derived)
}

// readDesignFiles is the design file map production lists at HEAD: every file
// under designDir, keyed by its forward-slash path relative to it.
func readDesignFiles(designDir string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(designDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(designDir, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(body)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read design directory: %w", err)
	}
	return files, nil
}

// writeDerived applies the derivation's files with production's commit rules:
// a component design.json replaces one that must exist; a lifted dependency
// definition is created only when absent.
func writeDerived(designDir string, derived []spec.DerivedDesignFile) ([]string, error) {
	var changed []string
	for _, f := range derived {
		path := filepath.Join(designDir, filepath.FromSlash(f.Path))
		info, err := os.Stat(path)
		exists := err == nil
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return changed, err
		}
		mode := fs.FileMode(0o644)
		switch {
		case f.CreateOnly && exists:
			continue
		case f.CreateOnly:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return changed, err
			}
		case !exists:
			return changed, fmt.Errorf("%s missing on disk", f.Path)
		default:
			mode = info.Mode().Perm()
		}
		if err := os.WriteFile(path, []byte(f.Content), mode); err != nil {
			return changed, err
		}
		changed = append(changed, f.Path)
	}
	return changed, nil
}
