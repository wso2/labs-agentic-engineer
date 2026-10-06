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

import { ANSWER_PREFIX, ANSWERS_PREFIX } from "@aep/agent-stream";
import type { ScriptedTurn } from "./interview";
import { Script } from "./interview";

// The mock Issues agent, on the Issues Page's chat. It does what the real one
// does (services/agents/src/agents/issues): classify the report, look for an
// issue that already covers it, draft one, ask "File this issue?" and file it
// on the user's File it. A report it cannot place gets the kind question
// first. It reads the conversation from the user's earlier messages, as the
// real agent reads its thread; nothing here mentions the classifier or a score.

type IssueKind = "bug" | "feature" | "improvement";

/** What the agent filed: the issue as the Issues list will show it. */
export interface FiledIssue {
  title: string;
  body: string;
  kind: IssueKind;
}

type IssuesTurn = ScriptedTurn & { filed?: FiledIssue };

const FILE_QUESTION = "File this issue?";
const FILE_IT = "File it";
const CHANGE_IT = "Change it";
const KIND_QUESTION = "What kind of issue is this?";
const KIND_LABELS: Record<string, IssueKind> = { Bug: "bug", "Feature request": "feature", Improvement: "improvement" };
const KIND_NAMES: Record<IssueKind, string> = { bug: "bug", feature: "feature request", improvement: "improvement" };

/** Sure of the kind at or above this; below it the agent asks (the real classifier's 0.8). */
const CLARIFY_BELOW = 0.8;

/**
 * A keyword reading of a report: broken-sounding words are a bug (0.97), wishes
 * a feature (0.93), anything else an improvement it is not sure of (0.75).
 */
function classify(report: string): { kind: IssueKind; confidence: number } {
  if (/\b(broken|not working|doesn'?t work|does nothing|errors?|crash\w*|fail\w*|wrong|bug)\b/i.test(report)) return { kind: "bug", confidence: 0.97 };
  if (/\b(add|would like|wish|support|could we|feature)\b/i.test(report)) return { kind: "feature", confidence: 0.93 };
  return { kind: "improvement", confidence: 0.75 };
}

const isAnswer = (message: string) => message.startsWith(ANSWER_PREFIX) || message.startsWith(ANSWERS_PREFIX);

function answerTo(question: string, message: string): string | null {
  const prefix = `${ANSWER_PREFIX}${question}": `;
  return message.startsWith(prefix) ? message.slice(prefix.length).split(" — ")[0]!.trim() : null;
}

/** The report being worked on and the kind it stands as, from the user's earlier messages in this conversation. */
function draftContext(history: string[]): { report?: string; kind?: IssueKind } {
  let chosen: IssueKind | undefined;
  for (let i = history.length - 1; i >= 0; i--) {
    const message = history[i]!.trim();
    if (!isAnswer(message)) {
      return { report: message, kind: chosen ?? classify(message).kind };
    }
    const label = answerTo(KIND_QUESTION, message);
    if (label && chosen === undefined) chosen = KIND_LABELS[label];
  }
  return chosen ? { kind: chosen } : {};
}

function sentence(report: string): string {
  return report.replace(/\s+/g, " ").trim().replace(/[.!?]+$/, "");
}

function titleOf(report: string): string {
  const text = sentence(report).replace(/^(the|a|an)\s+/i, "");
  const title = text.length > 72 ? `${text.slice(0, 69).trimEnd()}...` : text;
  return title.charAt(0).toUpperCase() + title.slice(1);
}

function draft(report: string, kind: IssueKind): FiledIssue {
  const said = sentence(report);
  const body =
    kind === "bug"
      ? `## What happened\n\n${said}.\n\n## What I expected\n\nIt should work as intended.\n\n## Steps to reproduce\n\n1. Open the screen this report is about.\n2. Try it as described above.`
      : kind === "feature"
        ? `## The need\n\n${said}.\n\n## The outcome\n\nThe product supports this, so the people who asked can do it without a workaround.`
        : `## The need\n\n${said}.\n\n## The outcome\n\nThe existing behaviour is smoother and easier to use.`;
  return { title: titleOf(report), body, kind };
}

/** The draft as the chat shows it: plain lines, as the chat does not render markdown. */
function plainDraft(report: string, kind: IssueKind): string {
  const said = sentence(report);
  if (kind === "bug") return `What happened: ${said}.\nWhat you expected: it works as intended.`;
  return `The need: ${said}.\nThe outcome: the product handles it, so no workaround is needed.`;
}

let counter = 0;

/** What the mock Issues agent does with a message, given the user's earlier messages in the conversation. */
export function scriptIssuesTurn(instruction: string, nextNumber: number, history: string[] = []): IssuesTurn {
  const text = instruction.trim();
  counter += 1;
  const id = (step: string) => `issues-${Date.now().toString(36)}-${counter}-${step}`;
  const progress = undefined;

  // File it: file what was drafted from the report before.
  if (answerTo(FILE_QUESTION, text) === FILE_IT) {
    const { report, kind } = draftContext(history);
    const filed = draft(report ?? "Something is not working as expected", kind ?? "bug");
    const s = new Script()
      .pause(400)
      .call(id("create"), "create_issue", { title: filed.title, body: filed.body, kind: filed.kind }, { number: nextNumber, title: filed.title })
      .say(`Filed #${nextNumber}: ${filed.title}. You can find it under Open in the list.`);
    return { display: text, ...s.end(), progress, filed };
  }

  if (answerTo(FILE_QUESTION, text) === CHANGE_IT) {
    const s = new Script().pause(300).say("Sure. Tell me what to change and I'll redraft the issue.");
    return { display: text, ...s.end(), progress };
  }

  // The kind question's answer: draft the report as that kind.
  const chosen = KIND_LABELS[answerTo(KIND_QUESTION, text) ?? ""];
  if (chosen) {
    const { report } = draftContext(history);
    return { display: text, ...draftAndAsk(new Script().pause(300), id, report ?? text, chosen).end(), progress };
  }

  if (isAnswer(text)) {
    return { display: text, ...new Script().pause(300).say("Got it.").end(), progress };
  }

  // A report: place it, then draft it or ask what kind it is.
  const { kind, confidence } = classify(text);
  const needsClarification = confidence < CLARIFY_BELOW;
  const alternatives = (["bug", "feature", "improvement"] as const).filter((k) => k !== kind).map((k) => ({ kind: k, confidence: Number(((1 - confidence) / 2).toFixed(2)) }));
  const s = new Script()
    .pause(500)
    .call(id("classify"), "classify_report", { message: text }, { kind, confidence, alternatives, needsClarification });
  if (needsClarification) {
    s.say("I can't tell yet what kind of issue this is.").ask(id("kind"), {
      question: KIND_QUESTION,
      detail: "It decides how I write the issue up.",
      options: [
        { label: "Bug", description: "Something that should work does not." },
        { label: "Feature request", description: "Something new the product should do." },
        { label: "Improvement", description: "Something that works but could be better." },
      ],
    });
    return { display: text, ...s.end(), progress };
  }
  return { display: text, ...draftAndAsk(s, id, text, kind).end(), progress };
}

/** Look for a duplicate, show the draft, and ask to file it. */
function draftAndAsk(s: Script, id: (step: string) => string, report: string, kind: IssueKind): Script {
  const issue = draft(report, kind);
  return s
    .call(id("search"), "search_issues", { query: issue.title, state: "open" }, { issues: [] })
    .say(`This reads as a ${KIND_NAMES[kind]}, and no open issue covers it. Here is the draft.`)
    .say(`Title: ${issue.title}`)
    .say(plainDraft(report, kind))
    .ask(id("file"), {
      question: FILE_QUESTION,
      options: [
        { label: FILE_IT, recommended: true, description: "Create the issue as drafted." },
        { label: CHANGE_IT, description: "Tell me what to change first." },
      ],
    });
}
