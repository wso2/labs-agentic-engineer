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

// Package skills embeds the bundled built-in SKILL.md files into the BFF
// binary so SkillBootstrap.Run() can UPSERT them into the `skills` table
// at startup without depending on a checked-out source tree.
//
// See docs/design/skills-system.md > "Bootstrap".
package skills

import "embed"

//go:embed builtin/*/SKILL.md
var BuiltinFS embed.FS

// PlannerFS carries the planner-facing built-in skills that are NOT part of the
// design-attachable catalogue and are never bootstrapped into the `skills`
// table. Today this is the `task-breakdown` skill the BFF pushes on every
// task-planner plan/detail call (mirrors how BuiltinFS backs the architect's
// builtins, but pushed on the wire directly rather than via the DB catalogue).
// See docs/design/skills-system.md and skills/task-breakdown/SKILL.md.
//
//go:embed planner/task-breakdown/SKILL.md
var PlannerFS embed.FS

// TaskBreakdownSkillPath is the embedded path of the task-breakdown SKILL.md.
const TaskBreakdownSkillPath = "planner/task-breakdown/SKILL.md"

// ArchitectFS carries the architect-facing built-in skills that are pushed on
// every architect (design) call directly on the wire — NOT bootstrapped into
// the `skills` table (unlike BuiltinFS). Today this is the
// `high-level-architecture` skill: it is authoring guidance for the design
// itself (component decomposition + dependency edges), not a design-attachable
// per-component coding skill, so it never enters the org catalogue. This
// mirrors PlannerFS (task-breakdown pushed to the planner) rather than
// BuiltinFS (the four stack skills bootstrapped to the catalogue).
// See docs/decisions/ADR-0005-single-skill-library.md and
// skills/high-level-architecture/SKILL.md (the single authored source; the
// embedded copy below is its go:embed-only mirror).
//
//go:embed architect/high-level-architecture/SKILL.md
var ArchitectFS embed.FS

// HighLevelArchitectureSkillPath is the embedded path of the
// high-level-architecture SKILL.md.
const HighLevelArchitectureSkillPath = "architect/high-level-architecture/SKILL.md"
