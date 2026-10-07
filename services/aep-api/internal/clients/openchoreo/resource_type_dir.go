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

package openchoreo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ResourceTypeFileName is the manifest each catalog directory entry holds:
// `<dir>/<type>/resourcetype.yaml` (deployments/single-cluster/resource-types).
const ResourceTypeFileName = "resourcetype.yaml"

// ResourceTypeDir is a ClusterResourceType source read off manifest files
// instead of the OC API: every `<dir>/*/resourcetype.yaml`, each a multi-document
// YAML stream whose ClusterResourceType document is decoded into the SAME
// ResourceType struct ListClusterResourceTypes returns.
//
// The decode goes YAML → generic value → JSON → ResourceType, so the struct's
// json tags are the only schema and a field the API would carry is read the
// same way here.
type ResourceTypeDir string

// ListClusterResourceTypes decodes every resource-type document under the
// directory; none is an error: an empty catalog would silently skip the
// derivation's membership check.
func (d ResourceTypeDir) ListClusterResourceTypes(_ context.Context) ([]ResourceType, error) {
	files, err := filepath.Glob(filepath.Join(string(d), "*", ResourceTypeFileName))
	if err != nil {
		return nil, fmt.Errorf("list resource types in %q: %w", string(d), err)
	}
	sort.Strings(files)
	var out []ResourceType
	for _, file := range files {
		types, err := decodeResourceTypeFile(file)
		if err != nil {
			return nil, err
		}
		out = append(out, types...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no ClusterResourceType found under %q (want */%s)", string(d), ResourceTypeFileName)
	}
	return out, nil
}

// decodeResourceTypeFile returns the resource-type documents of one manifest.
func decodeResourceTypeFile(file string) ([]ResourceType, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", file, err)
	}
	defer func() { _ = f.Close() }()

	var out []ResourceType
	dec := yaml.NewDecoder(f)
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode %q: %w", file, err)
		}
		kind, _ := doc["kind"].(string)
		if kind != "Cluster"+kindResourceType && kind != kindResourceType {
			continue
		}
		apiVersion, _ := doc["apiVersion"].(string)
		if !strings.HasPrefix(apiVersion, "openchoreo.dev/") {
			continue
		}
		raw, err := json.Marshal(doc)
		if err != nil {
			return nil, fmt.Errorf("re-encode %q: %w", file, err)
		}
		var rt ResourceType
		if err := json.Unmarshal(raw, &rt); err != nil {
			return nil, fmt.Errorf("decode %s in %q: %w", kind, file, err)
		}
		out = append(out, rt)
	}
	return out, nil
}
