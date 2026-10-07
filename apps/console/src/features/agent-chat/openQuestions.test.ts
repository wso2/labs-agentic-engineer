/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

import { describe, expect, it } from "vitest";
import type { ProjectCard, ProjectPage, ShellScope } from "../shell/scope";
import { opensQuestionsCard } from "./openQuestions";

const at = (page: ProjectPage, card: ProjectCard | null = null, projectName = "acme"): ShellScope => ({
  kind: "project",
  projectName,
  page,
  card,
  specFile: null,
});

describe("opensQuestionsCard (where asked questions open the card)", () => {
  it("opens over the project's overview and the cards on it", () => {
    expect(opensQuestionsCard(at("overview"), "acme")).toBe(true);
    expect(opensQuestionsCard(at("overview", "spec"), "acme")).toBe(true);
    expect(opensQuestionsCard(at("overview", "design"), "acme")).toBe(true);
    expect(opensQuestionsCard(at("overview", "prototype"), "acme")).toBe(true);
  });

  it("leaves the user where they are on the project's other pages", () => {
    expect(opensQuestionsCard(at("builds"), "acme")).toBe(false);
    expect(opensQuestionsCard(at("builds", "build"), "acme")).toBe(false);
    expect(opensQuestionsCard(at("deploy", "configure"), "acme")).toBe(false);
    expect(opensQuestionsCard(at("issues"), "acme")).toBe(false);
  });

  it("does nothing for another project, the org, or a card already open", () => {
    expect(opensQuestionsCard(at("overview", null, "other"), "acme")).toBe(false);
    expect(opensQuestionsCard({ kind: "org", page: "dashboard", card: null }, "acme")).toBe(false);
    expect(opensQuestionsCard(at("overview", "questions"), "acme")).toBe(false);
  });
});
