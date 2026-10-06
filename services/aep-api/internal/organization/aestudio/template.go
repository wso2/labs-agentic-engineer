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

// Package aestudio installs and converges an org's AE Studio (the ae-studio
// Resource: ResourceType, Project ae-system, bindings), ADR-0040.
package aestudio

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

//go:generate cp ../../../../../components/dataplane/ae-system-project/ae-studio/resourcetype.yaml resourcetype.yaml

//go:embed resourcetype.yaml
var templateYAML []byte

// Template parses the embedded ae-studio ResourceType.
func Template() (*openchoreo.ResourceType, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(templateYAML, &doc); err != nil {
		return nil, fmt.Errorf("ae-studio template: %w", err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("ae-studio template: %w", err)
	}
	rt := &openchoreo.ResourceType{}
	if err := json.Unmarshal(raw, rt); err != nil {
		return nil, fmt.Errorf("ae-studio template: %w", err)
	}
	return rt, nil
}

// TemplateHash is the first 16 hex characters of the sha256 of the embedded
// bytes; it stamps the installed ResourceType so a changed template is noticed.
func TemplateHash() string {
	s := sha256.Sum256(templateYAML)
	return hex.EncodeToString(s[:])[:16]
}
