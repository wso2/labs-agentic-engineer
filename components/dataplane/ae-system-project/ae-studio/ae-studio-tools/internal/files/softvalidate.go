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

// Moved from services/aep-api/internal/spec/files_service.go (softValidate);
// the aep-api copy is deleted in phase 4.

package files

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/wso2/aep/ae-studio-tools/internal/designspec"
	"github.com/wso2/aep/ae-studio-tools/internal/securityspec"
)

// softValidate returns non-blocking warnings for a written file (the hard
// semantic gate stays at save/tag). A component design.json or dependency
// dependency.json is validated against its published schema plus the
// name==dir rule; security.json is parsed with the platform's own rules; any
// other .json gets a cheap parseability check. Warnings never block the
// commit.
func softValidate(path, content string) []Warning {
	// The security.json document is the ONE spec file the platform later acts
	// on deterministically (directory roles and test users at build time), so
	// a malformed one is worth flagging the moment it is written.
	if path == securityspec.Path {
		if _, err := securityspec.Parse([]byte(content)); err != nil {
			var ve *securityspec.ValidationError
			if errors.As(err, &ve) {
				return []Warning{{Path: path, Code: ve.Code, Message: ve.Message}}
			}
			return []Warning{{Path: path, Code: securityspec.CodeSchemaViolation, Message: err.Error()}}
		}
		return nil
	}
	if dir, ok := componentDesignDir(path); ok {
		if err := designspec.ValidateComponentDesignInDir([]byte(content), dir); err != nil {
			var ve *designspec.ValidationError
			if errors.As(err, &ve) {
				return []Warning{{Path: path, Code: ve.Code, Message: ve.Message}}
			}
		}
		return nil
	}
	if dir, ok := dependencyFileDir(path); ok {
		if err := designspec.ValidateDependencyDesignInDir([]byte(content), dir); err != nil {
			var ve *designspec.ValidationError
			if errors.As(err, &ve) {
				return []Warning{{Path: path, Code: ve.Code, Message: ve.Message}}
			}
		}
		return nil
	}
	if strings.HasSuffix(path, ".json") && !json.Valid([]byte(content)) {
		return []Warning{{Path: path, Code: designspec.CodeInvalidJSON, Message: "content is not valid JSON"}}
	}
	return nil
}

// componentDesignDir returns the <name> directory of a component design.json
// path (specs/design/components/<name>/design.json), and whether path is one.
func componentDesignDir(path string) (string, bool) {
	const prefix = "specs/design/components/"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, "/design.json") {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) != 2 {
		return "", false
	}
	return parts[0], true
}
