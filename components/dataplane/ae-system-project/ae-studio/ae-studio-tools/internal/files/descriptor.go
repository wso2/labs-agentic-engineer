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

package files

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// The project descriptor (aep-api writes it at project create):
// specs/.agentic-engineer.toml carries the idea the user typed. Its
// dot-led segment keeps it out of what the agent reads from its snapshot, so
// the lookup reads the idea here and answers it, and a `/start` turn in the
// agent uses it when no idea is typed inline.

// DescriptorPath is the descriptor's repo-relative path (aep-api's
// spec.DescriptorPath).
const DescriptorPath = "specs/.agentic-engineer.toml"

// descriptor is the one descriptor field the lookup reads.
type descriptor struct {
	Idea string `toml:"idea"`
}

// projectIdea reads the captured idea at sha, trimmed. Best-effort, as
// aep-api's readProjectIdea was: no descriptor, an unreadable one or a
// malformed one answer "", because a lost idea costs the user one question
// from the start skill while a failed lookup costs them the turn. The log
// lines carry no descriptor content (a parse error can quote it).
func (r Reader) projectIdea(ctx context.Context, ref repo.RepoRef, project, sha string) string {
	raw, _, err := r.Engine.ReadFile(ctx, ref, sha, DescriptorPath)
	if errors.Is(err, repo.ErrPathNotFound) {
		return ""
	}
	if err != nil {
		slog.WarnContext(ctx, "snapshot.idea_unreadable", "project", project, "step", "read", "error", err)
		return ""
	}
	var d descriptor
	if _, err := toml.Decode(string(raw), &d); err != nil {
		slog.WarnContext(ctx, "snapshot.idea_unreadable", "project", project, "step", "parse")
		return ""
	}
	return strings.TrimSpace(d.Idea)
}
