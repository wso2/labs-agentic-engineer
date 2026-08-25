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

// The transcript's opening line (#562/#528).
//
// The platform fires the kickoff at project creation, so the first thing in
// every project's transcript is a `/start` nobody typed — and the idea riding
// it is the user's own words, attached server-side from the project descriptor.
// Rendering that as one long command string would bury the words the line
// exists to show, so the panel splits it: the command reads as a command, and
// the idea reads as prose beside it, cropped.
//
// A DISPLAY concern only. The crop lives in CSS (see MessageList) rather than
// here, because a character count cannot know the panel's width — which the
// user drags — and a truncated string would also be what a screen reader and a
// copy both got. This module only decides WHICH message is a start line and
// where the command ends.

import { START_COMMAND } from "@aep/contracts/commands";

/** A `/start` transcript line, split into its parts. */
export interface StartLine {
  /** The user's own words, as the server resolved them; "" when it had none. */
  idea: string;
}

/**
 * Reads a user message as a start line, or null when it is anything else.
 *
 * Deliberately narrow — an exact `/start`, optionally followed by whitespace
 * and free text. The server's own grammar is the authority on what a command
 * is (`slashCommandPattern` in aep-api); this is not a second parser, only the
 * recognition of the one command the panel renders specially. Anything it does
 * not recognise renders as ordinary text, which is always safe.
 */
export function startLineOf(content: string): StartLine | null {
  const trimmed = content.trim();
  if (trimmed === START_COMMAND) return { idea: "" };
  if (!trimmed.startsWith(START_COMMAND)) return null;
  const rest = trimmed.slice(START_COMMAND.length);
  // A boundary is required: `/started` is prose, not a command with an idea.
  if (!/^\s/.test(rest)) return null;
  return { idea: rest.trim() };
}
