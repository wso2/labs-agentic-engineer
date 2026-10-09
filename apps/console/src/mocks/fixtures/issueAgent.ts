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

import { ANSWER_PREFIX } from "@aep/agent-stream";
import type { ScriptedTurn } from "./interview";
import { Script } from "./interview";

// The mock agent of one issue, on that issue's own chat. It does what the real
// one does (services/agents/src/agents/issue): read the issue, talk about it,
// and change it only once the user has confirmed exactly that change. Each
// change is drafted from the user's words, shown as its confirm option's
// description on one question card (the agents service's gate binds the
// answer to that text), and made on that option alone: a comment ("Post this
// comment?" · Post it), an edit ("Apply this edit?" · Apply it), closing
// ("Close this issue?" · Close it) or handing it to the coding agent ("Hand
// this to the coding agent?" · Hand it over). Not now changes nothing.

/** The issue as its agent reads it. */
export interface MockIssue {
  number: number;
  title: string;
  body: string;
  state: "open" | "closed";
}

/** What a confirmed answer changed on the issue, for the mock's issue list. */
export type IssueChange =
  | { kind: "comment"; body: string }
  | { kind: "edit"; title: string }
  | { kind: "close"; reason: string };

type IssueTurn = Omit<ScriptedTurn, "progress"> & { progress: undefined; change?: IssueChange };

/** The design's components the coding agent can work in (the real agent lists them). */
const COMPONENTS = ["expense-api", "expense-web"];
const NOT_NOW = "Not now";

/** One confirmable change: its question, its go-ahead option, and how it is read from the user's words. */
interface Write {
  tool: "comment_issue" | "edit_issue" | "close_issue" | "hand_to_coding_agent";
  question: string;
  option: string;
  matches: RegExp;
  /** The tool's arguments, drafted from the request. */
  draft: (request: string) => Record<string, string>;
  /** The arguments as the option's description shows them (`describeChange` in the agents service). */
  describe: (args: Record<string, string>) => string;
  change?: (args: Record<string, string>) => IssueChange;
}

/** The request with its leading command words cut, as a sentence. */
function rest(request: string, lead: RegExp, fallback: string): string {
  const text = request.trim().replace(lead, "").replace(/^[,:\s]+/, "").trim();
  if (!text) return fallback;
  const sentence = text.charAt(0).toUpperCase() + text.slice(1);
  return /[.!?]$/.test(sentence) ? sentence : `${sentence}.`;
}

const WRITES: Write[] = [
  {
    tool: "close_issue",
    question: "Close this issue?",
    option: "Close it",
    matches: /\bclose\b/i,
    draft: (r) => ({ reason: rest(r, /^(please\s+)?close( it| this( issue)?)?( because| as)?/i, "Done.") }),
    describe: (a) => a.reason!,
    change: (a) => ({ kind: "close", reason: a.reason! }),
  },
  {
    tool: "hand_to_coding_agent",
    question: "Hand this to the coding agent?",
    option: "Hand it over",
    matches: /\b(coding agent|hand)\b/i,
    draft: () => ({ component: COMPONENTS[0]! }),
    describe: (a) => `Component: ${a.component}`,
  },
  {
    tool: "edit_issue",
    question: "Apply this edit?",
    option: "Apply it",
    matches: /\b(rename|retitle|title)\b/i,
    draft: (r) => ({ title: r.trim().replace(/^(please\s+)?(rename|retitle|change the title of)( it| this( issue)?)?( to)?\s*/i, "").trim() }),
    describe: (a) => `Title: ${a.title}`,
    change: (a) => ({ kind: "edit", title: a.title! }),
  },
  {
    tool: "comment_issue",
    question: "Post this comment?",
    option: "Post it",
    matches: /\b(comment|reply|post)\b/i,
    draft: (r) => ({ body: rest(r, /^(please\s+)?(post |add |leave )?(a )?comment( on it)?( saying| that)?/i, "Thanks, looking into it.") }),
    describe: (a) => a.body!,
    change: (a) => ({ kind: "comment", body: a.body! }),
  },
];

const isAnswer = (message: string) => message.startsWith(ANSWER_PREFIX);

function answerTo(question: string, message: string): string | null {
  const prefix = `${ANSWER_PREFIX}${question}": `;
  return message.startsWith(prefix) ? message.slice(prefix.length).split(" — ")[0]!.trim() : null;
}

/** The user's last request before this answer: what the change was drafted from. */
function lastRequest(history: string[]): string | undefined {
  return [...history].reverse().find((m) => !isAnswer(m.trim()));
}

let counter = 0;

/** What the mock agent of `issue` does with a message, given the user's earlier messages in its chat. */
export function scriptIssueTurn(instruction: string, issue: MockIssue, history: string[] = []): IssueTurn {
  const text = instruction.trim();
  counter += 1;
  const id = (step: string) => `issue-${issue.number}-${Date.now().toString(36)}-${counter}-${step}`;
  const read = (s: Script) =>
    s.mcp(id("read"), "get_issue", {}, JSON.stringify({ number: issue.number, title: issue.title, state: issue.state, body: issue.body, comments: [] }));

  // An answer to a confirmation: make exactly the drafted change, or nothing.
  for (const write of WRITES) {
    const answer = answerTo(write.question, text);
    if (answer === null) continue;
    if (answer !== write.option) {
      return { display: text, ...new Script().pause(300).say("Okay, I left the issue as it is.").end(), progress: undefined };
    }
    const args = write.draft(lastRequest(history) ?? "");
    const s = new Script().pause(400).mcp(id("write"), write.tool, args, done(write.tool, issue.number));
    s.say(saidDone(write.tool, issue.number));
    return { display: text, ...s.end(), progress: undefined, ...(write.change ? { change: write.change(args) } : {}) };
  }

  // A request for a change: read the issue, draft it, and ask before making it.
  const write = WRITES.find((w) => w.matches.test(text));
  if (write) {
    const s = read(new Script().pause(400));
    if (write.tool === "hand_to_coding_agent") s.mcp(id("components"), "list_components", {}, JSON.stringify({ components: COMPONENTS }));
    const args = write.draft(text);
    s.say(`Here is what I'd do on #${issue.number}.`).ask(id("confirm"), {
      question: write.question,
      options: [
        { label: write.option, description: write.describe(args) },
        { label: NOT_NOW, description: "Leave the issue as it is." },
      ],
    });
    return { display: text, ...s.end(), progress: undefined };
  }

  // Anything else: read the issue and talk about it.
  const s = read(new Script().pause(400)).say(
    `#${issue.number} is "${issue.title}", ${issue.state}. I can comment on it, edit it, close it or hand it to the coding agent.`,
  );
  return { display: text, ...s.end(), progress: undefined };
}

/** The tool's text result, as aep-api's issue tools answer. */
function done(tool: Write["tool"], number: number): string {
  if (tool === "comment_issue") return `Commented on #${number}.`;
  if (tool === "edit_issue") return `Edited #${number}.`;
  if (tool === "close_issue") return `Closed #${number}.`;
  return `Handed #${number} to the coding agent.`;
}

function saidDone(tool: Write["tool"], number: number): string {
  if (tool === "comment_issue") return `Posted the comment on #${number}.`;
  if (tool === "edit_issue") return `Updated #${number}.`;
  if (tool === "close_issue") return `Closed #${number}. Its chat is removed now that it is closed.`;
  return `Handed #${number} to the coding agent. Its log shows on the issue's card.`;
}
