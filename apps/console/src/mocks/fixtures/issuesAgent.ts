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

import { ANSWER_PREFIX, ANSWERS_PREFIX, type AskQuestionInput } from "@aep/agent-stream";
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
const ABOUT_QUESTION = "What should the issue be about?";
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

/** An answer's text in a batch (`Answers:` lines), or null when the batch does not answer the question. */
function batchAnswerTo(question: string, message: string): string | null {
  if (!message.startsWith(ANSWERS_PREFIX)) return null;
  const prefix = `- "${question}": `;
  const line = message.split("\n").find((l) => l.startsWith(prefix));
  return line ? line.slice(prefix.length).split(" — ")[0]!.trim() : null;
}

/** What follows /issue ("" for a bare /issue), or null when the message is not the command. */
function issueText(message: string): string | null {
  const match = /^\/issue(?:\s+([\s\S]*))?$/.exec(message.trim());
  return match ? (match[1] ?? "").trim() : null;
}

/** The kind the user chose in an answer, from the kind question alone or in a batch. */
function chosenKind(message: string): IssueKind | undefined {
  return KIND_LABELS[answerTo(KIND_QUESTION, message) ?? batchAnswerTo(KIND_QUESTION, message) ?? ""];
}

/**
 * The report being worked on and the kind it stands as, from the user's earlier
 * messages in this conversation. A /issue report is the text after the command
 * (or the answer to what the issue should be about); `viaIssue` marks it.
 */
function draftContext(history: string[]): { report?: string; kind?: IssueKind; viaIssue?: boolean } {
  let chosen: IssueKind | undefined;
  let about: string | undefined;
  for (let i = history.length - 1; i >= 0; i--) {
    const message = history[i]!.trim();
    if (!isAnswer(message)) {
      const command = issueText(message);
      const report = command === null ? message : command || about;
      return report ? { report, kind: chosen ?? classify(report).kind, viaIssue: command !== null } : {};
    }
    chosen ??= chosenKind(message);
    about ??= answerTo(ABOUT_QUESTION, message) ?? undefined;
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

  // The answers to a /issue's clarifying batch: draft the report with them.
  if (text.startsWith(ANSWERS_PREFIX)) {
    const { report, kind, viaIssue } = draftContext(history);
    if (report && viaIssue) {
      return { display: text, ...draftAndAsk(new Script().pause(300), id, report, chosenKind(text) ?? kind ?? "bug").end(), progress };
    }
  }

  if (isAnswer(text)) {
    // The answer to a bare /issue's question is the report itself.
    const about = answerTo(ABOUT_QUESTION, text);
    if (!about) return { display: text, ...new Script().pause(300).say("Got it.").end(), progress };
    return { display: text, ...issueReport(id, about).end(), progress };
  }

  // /issue: the user has decided to file. A bare one asks what it is about; otherwise
  // classify the text without the command, and ask only what is missing, in one batch.
  const command = issueText(text);
  if (command === "") {
    const s = new Script().pause(300).ask(id("about"), { question: ABOUT_QUESTION, options: [] });
    return { display: text, ...s.end(), progress };
  }
  if (command !== null) return { display: text, ...issueReport(id, command).end(), progress };

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

/** The questions a /issue report leaves open: what was not said, free-text, at most 4. */
function missingQuestions(kind: IssueKind, needsKind: boolean): AskQuestionInput[] {
  const free = (question: string, detail?: string): AskQuestionInput => ({ question, ...(detail ? { detail } : {}), options: [] });
  const asked: AskQuestionInput[] =
    kind === "bug"
      ? [
          free("What did you expect to happen?"),
          free("Where does it happen?", "The page or screen."),
          free("What are the steps to reproduce it?"),
        ]
      : [free("What is the need behind this?"), free("What outcome do you want?")];
  if (!needsKind) return asked;
  const kindQuestion: AskQuestionInput = {
    question: KIND_QUESTION,
    detail: "It decides how I write the issue up.",
    options: [
      { label: "Bug", description: "Something that should work does not." },
      { label: "Feature request", description: "Something new the product should do." },
      { label: "Improvement", description: "Something that works but could be better." },
    ],
  };
  return [kindQuestion, free("What outcome do you want?")];
}

/**
 * A /issue report: classify its text (without the command), then the one batch of what is missing.
 * The mock simplifies: it always asks its batch, where the real agent skips what the report already answers.
 */
function issueReport(id: (step: string) => string, report: string): Script {
  const { kind, confidence } = classify(report);
  const needsClarification = confidence < CLARIFY_BELOW;
  const alternatives = (["bug", "feature", "improvement"] as const).filter((k) => k !== kind).map((k) => ({ kind: k, confidence: Number(((1 - confidence) / 2).toFixed(2)) }));
  return new Script()
    .pause(500)
    .call(id("classify"), "classify_report", { message: report }, { kind, confidence, alternatives, needsClarification })
    .say("A few questions, so the issue is complete.")
    .askAll(id("missing"), missingQuestions(kind, needsClarification).slice(0, 4));
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
