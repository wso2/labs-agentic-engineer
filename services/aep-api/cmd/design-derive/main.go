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

// Command design-derive runs aep-api's platform-resource derivation over a
// design directory on disk, with a resource-type catalog read off manifest
// files — the step POST /build runs before it cuts a tag, for a project that is
// never built (the playground).
//
//	design-derive --design-dir <project>/specs/design --project <id> [--resource-types <dir>]
//
// See playground ADR-0003.
//
// Exit status: 0 derived (or nothing to derive), 1 the derivation refused the
// design (production's own error text on stderr), 2 a usage or I/O failure.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// catalogInRepo is where the default catalog lives, relative to the repo root.
const catalogInRepo = "deployments/single-cluster/resource-types"

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("design-derive", flag.ContinueOnError)
	fs.SetOutput(stderr)
	designDir := fs.String("design-dir", "", "the project's specs/design directory (required)")
	projectID := fs.String("project", "", "the project id: the OC name prefix of every derived ref (required)")
	typesDir := fs.String("resource-types", "", "the resource-type catalog: <dir>/*/resourcetype.yaml (default: the repo's "+catalogInRepo+", found from the working directory)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *designDir == "" || *projectID == "" || fs.NArg() != 0 {
		fs.Usage()
		return 2
	}
	if *typesDir == "" {
		found, err := findRepoCatalog()
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "design-derive: %v; pass --resource-types\n", err)
			return 2
		}
		*typesDir = found
	}

	changed, err := deriveDir(ctx, *designDir, *projectID, *typesDir)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "design-derive: %v\n", err)
		var refusal refusedError
		if errors.As(err, &refusal) {
			return 1
		}
		return 2
	}
	if len(changed) == 0 {
		_, _ = fmt.Fprintln(stdout, "derived: no change")
	}
	for _, path := range changed {
		_, _ = fmt.Fprintf(stdout, "derived: %s\n", path)
	}
	return 0
}

// findRepoCatalog walks up from the working directory to the first ancestor
// that holds the repo's catalog.
func findRepoCatalog() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, catalogInRepo)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s above the working directory", catalogInRepo)
		}
		dir = parent
	}
}
