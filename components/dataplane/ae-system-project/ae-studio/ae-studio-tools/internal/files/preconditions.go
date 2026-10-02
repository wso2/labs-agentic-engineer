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

// Moved from services/aep-api/internal/spec/files_service.go
// (checkPreconditions); the aep-api copy is deleted in phase 4.

package files

// checkPreconditions compares each op's baseSha against the current tree.
// baseSha == "" on a write means "must not exist"; on a delete it means
// "delete whatever is there" but the path must still exist.
//
//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
func checkPreconditions(req ApplyRequest, current map[string]string) []Conflict {
	var conflicts []Conflict
	for _, w := range req.Writes {
		cur, exists := current[w.Path]
		if w.BaseSHA == "" {
			if exists {
				conflicts = append(conflicts, Conflict{Path: w.Path, BaseSHA: "", CurrentSHA: cur})
			}
			continue
		}
		if !exists || cur != w.BaseSHA {
			conflicts = append(conflicts, Conflict{Path: w.Path, BaseSHA: w.BaseSHA, CurrentSHA: cur})
		}
	}
	for _, d := range req.Deletes {
		cur, exists := current[d.Path]
		if !exists {
			conflicts = append(conflicts, Conflict{Path: d.Path, BaseSHA: d.BaseSHA, CurrentSHA: ""})
			continue
		}
		if d.BaseSHA != "" && cur != d.BaseSHA {
			conflicts = append(conflicts, Conflict{Path: d.Path, BaseSHA: d.BaseSHA, CurrentSHA: cur})
		}
	}
	return conflicts
}
