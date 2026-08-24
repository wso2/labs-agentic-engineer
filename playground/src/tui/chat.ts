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

/**
 * The streaming chat screen (docs/design/playground.md §7) — the playground's
 * home surface. Free-chat turns run in the project's single `general`
 * conversation (exactly the console's chat panel); slash commands drive every
 * other phase without leaving chat. Follow-ups are history-aware; hand-edits
 * between turns are picked up via D20 (`filesChangedExternally`).
 *
 * Command taxonomy + precedence live in `chat-commands.ts` (pure); this module
 * is the I/O half: render the entry banner, prompt, and dispatch each intent.
 * Phase-runners reuse the existing engine commands as-is — a `/task` boots its
 * own short-lived task-plan session, a `/code` spawns the coding-agent
 * subprocess with its consent + undo snapshot — so readline is paused around
 * them (they own stdout, and `/code` prompts for consent).
 */

import readline from "node:readline/promises";
import { stdin as input, stdout as output } from "node:process";
import { chatTurn, codeCommand, tasksCommand, undoCommand, type PhaseOptions } from "../commands.js";
import { checkProject } from "../engine/check.js";
import { loadRepoSkills } from "../kit/skills.js";
import { collectAnswers } from "./questions.js";
import { buildChatBanner, commandGuide } from "./banner.js";
import { classifyChatInput, type ChatIntent } from "./chat-commands.js";
import { confirmCodingDir } from "./consent.js";
import { chatSpec, startSpec } from "../engine/turn-spec.js";
import { readReferences } from "../state/references.js";
import { readIdea } from "../state/descriptor.js";
import type { PlaygroundSession } from "../engine/session.js";

/** Run one phase-runner (`/task`, `/code`, `/validate`, `/undo`) to completion. */
async function runPhase(session: PlaygroundSession, intent: Extract<ChatIntent, { kind: "phase" }>, opts: PhaseOptions): Promise<void> {
  const dir = session.projectDir;
  switch (intent.name) {
    case "validate": {
      let allOk = true;
      for (const f of checkProject(dir)) {
        output.write(f.ok ? `  ✓ ${f.path} — ${f.message}\n` : `  ✗ ${f.path} — ${f.message}\n`);
        if (!f.ok) allOk = false;
      }
      output.write(allOk ? "  ✓ check passed\n" : "  ✗ check found problems\n");
      return;
    }
    case "undo": {
      const outcome = undoCommand(dir, opts); // prints `↺ restored …` itself on success
      if (!outcome.ok) output.write(`  ✗ ${outcome.detail ?? "undo failed"}\n`);
      return;
    }
    case "task": {
      const outcome = await tasksCommand(dir, opts); // streams its own fold summary
      if (!outcome.ok) output.write(`  ✗ tasks: ${outcome.detail ?? "failed"}\n`);
      return;
    }
    case "code": {
      // ONE session works the whole project (ADR-0001) — the `aep` skill picks
      // its own working set, so a per-issue argument has nothing to steer.
      // Consent + undo snapshot ride inside.
      if (intent.arg) {
        output.write("  ✗ /code takes no argument — one session works the whole plan (the agent picks its own working set)\n");
        return;
      }
      const outcome = await codeCommand(dir, opts, confirmCodingDir(dir));
      if (!outcome.ok) output.write(`  ✗ code: ${outcome.detail ?? "failed"}\n`);
      return;
    }
  }
}

/** The per-project chat loop. Returns when the user goes back to the menu or quits. */
export async function chatLoop(session: PlaygroundSession, opts: PhaseOptions): Promise<"menu" | "quit"> {
  const rl = readline.createInterface({ input, output });
  rl.on("SIGINT", () => {
    output.write("\n");
    process.exit(130); // Ctrl-C: hard exit (the in-process server dies with us)
  });
  const skillCount = loadRepoSkills(session.skillsDir).length;
  output.write(buildChatBanner(session.projectDir, session.state.slug, skillCount) + "\n");
  try {
    for (;;) {
      let raw: string;
      try {
        raw = await rl.question(`\n${session.state.slug} ❯ `);
      } catch {
        return "quit"; // stdin closed (Ctrl-D)
      }
      const line = raw.trim();
      if (line === "") continue;

      const intent = classifyChatInput(line);

      if (intent.kind === "control") {
        if (intent.name === "quit") return "quit";
        if (intent.name === "menu") return "menu";
        output.write(commandGuide() + "\n");
        continue;
      }

      if (intent.kind === "phase") {
        // The engine commands own stdout and (for /code) prompt for consent —
        // pause readline so it doesn't fight them for stdin, resume after.
        rl.pause();
        try {
          await runPhase(session, intent, opts);
        } finally {
          rl.resume();
        }
        continue;
      }

      // `/start` — the kickoff. The idea typed inline wins; otherwise it comes
      // from the descriptor written when the project was created. Neither is
      // something the agent could read for itself, so the turn spec is the only
      // channel it travels by. No idea at all is fine: the start skill opens by
      // asking for one.
      const turn =
        intent.kind === "start"
          ? startSpec(intent.inlineIdea ?? readIdea(session.projectDir), readReferences(session.projectDir))
          : intent.turn;

      // A skill-load (`/spec`, `/design`, `/<skill>`) or a plain chat turn.
      // HITL loop (console ADR-0012 / #270): while the agent ends a turn on a
      // question card, prompt for the answer and continue with it as the next
      // turn — an answer is ordinary chat, whatever the turn that asked was.
      // `/skip` (or Ctrl-D at the prompt) drops back to free chat.
      let outcome = await chatTurn(session, turn, opts);
      while (outcome.ok && outcome.pending) {
        const answer = await collectAnswers(rl, outcome.pending);
        if (answer === null) {
          output.write("  (question skipped)\n");
          break;
        }
        outcome = await chatTurn(session, chatSpec(answer), opts);
      }
      if (!outcome.ok) output.write(`\n[turn failed] ${outcome.detail ?? "unknown error"}\n`);
    }
  } finally {
    rl.close();
  }
}
