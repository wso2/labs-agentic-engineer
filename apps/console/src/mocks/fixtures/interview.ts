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

import {
  ANSWER_PREFIX,
  ANSWERS_PREFIX,
  ASK_QUESTION_TOOL,
  ASK_QUESTIONS_TOOL,
  type AskQuestionInput,
  type StreamPart,
} from "@aep/agent-stream";
import { parseInterviewCommand, START_COMMAND } from "@aep/contracts/commands";
import type { WireScope } from "../../features/agent-chat/turnScope";
import type { SpecFeature } from "../../features/spec/api/specModel";
import type { MockSpecModel } from "./spec";
import type { LineBlock } from "../../features/spec/model/ids";
import { blockingQuestions } from "../../features/spec/model/questions";
import type { components } from "../../generated/aep-api";
import type { InterviewEffect, InterviewProgress, ScriptFrame } from "../chatServer";

type ConversationMessage = components["schemas"]["ConversationMessage"];

// The mock agent's side of the conversation: what it answers each turn with,
// as a timed stream in the wire's own frames (@aep/agent-stream) and as the
// messages the history keeps afterwards.
//
// The per-feature interview: `/interview F<n>` scoped to that feature
// asks two spine questions, one card at a time; the second answer writes the
// feature's file (stub → interviewed, one `*assumed*` line) with an editFile
// the client applies to its local doc. Spending reports and Mileage claims
// have their own questions; any other feature gets a generic pair. Besides
// the interview: the kickoff (`/start`), a short answer on the product, and a
// short acknowledgement anywhere else.

/** Builds one turn: its frames on a clock, and the messages it persists. */
export class Script {
  private t = 0;
  private lastWasText = false;
  readonly frames: ScriptFrame[] = [];
  private readonly parts: unknown[] = [];
  private readonly results: unknown[] = [];

  private emit(part: StreamPart, gap: number): void {
    this.t += gap;
    this.frames.push({ at: this.t, part });
  }

  pause(ms: number): this {
    this.t += ms;
    return this;
  }

  /** Narration, streamed a few words at a time. A second paragraph in a row is set apart. */
  say(text: string): this {
    const body = this.lastWasText ? `\n\n${text}` : text;
    const words = body.split(/(?<=\s)/);
    for (let i = 0; i < words.length; i += 3) this.emit({ type: "text-delta", delta: words.slice(i, i + 3).join("") }, 60);
    const last = this.parts.at(-1) as { type?: string; text?: string } | undefined;
    if (last?.type === "text") last.text += body;
    else this.parts.push({ type: "text", text: body });
    this.lastWasText = true;
    return this;
  }

  /** One question card (ask_question); the turn ends waiting for its answer. */
  ask(toolCallId: string, input: AskQuestionInput): this {
    this.emit({ type: "tool-call", toolCallId, toolName: ASK_QUESTION_TOOL, input }, 200);
    const output = { status: "awaiting_user_response" };
    this.emit({ type: "tool-result", toolCallId, toolName: ASK_QUESTION_TOOL, input, output }, 30);
    this.parts.push({ type: "tool-call", toolCallId, toolName: ASK_QUESTION_TOOL, input });
    this.results.push({ type: "tool-result", toolCallId, toolName: ASK_QUESTION_TOOL, output: { type: "json", value: output } });
    this.lastWasText = false;
    return this;
  }

  /**
   * A batch of questions (ask_questions), its input streamed as the provider
   * streams it, so the card fills question by question; the turn ends waiting
   * for the answers.
   */
  askAll(toolCallId: string, questions: AskQuestionInput[]): this {
    const toolName = ASK_QUESTIONS_TOOL;
    const input = { questions };
    this.emit({ type: "tool-input-start", id: toolCallId, toolName }, 250);
    const json = JSON.stringify(input);
    // Paced so each question lands a beat after the last: long enough to see
    // the card say "Still asking…", short enough not to wait on it.
    const chunk = Math.max(24, Math.ceil(json.length / 30));
    for (let i = 0; i < json.length; i += chunk) this.emit({ type: "tool-input-delta", id: toolCallId, delta: json.slice(i, i + chunk) }, 80);
    this.emit({ type: "tool-input-end", id: toolCallId }, 60);
    this.emit({ type: "tool-call", toolCallId, toolName, input }, 30);
    const output = { status: "awaiting_user_response" };
    this.emit({ type: "tool-result", toolCallId, toolName, input, output }, 30);
    this.parts.push({ type: "tool-call", toolCallId, toolName, input });
    this.results.push({ type: "tool-result", toolCallId, toolName, output: { type: "json", value: output } });
    this.lastWasText = false;
    return this;
  }

  /** An editFile, its input streamed as the provider streams it, then its verdict. */
  edit(toolCallId: string, path: string, oldString: string, newString: string): this {
    const input = { path, oldString, newString };
    return this.write(toolCallId, "editFile", input);
  }

  /** An addFile: a new file, whole. */
  add(toolCallId: string, path: string, content: string): this {
    const input = { path, content };
    return this.write(toolCallId, "addFile", input);
  }

  private write(toolCallId: string, toolName: "editFile" | "addFile", input: { path: string }): this {
    const { path } = input;
    const op = toolName === "editFile" ? "edit" : "add";
    this.emit({ type: "tool-input-start", id: toolCallId, toolName }, 250);
    const json = JSON.stringify(input);
    // Paced as a model writes: slow enough to watch the line say "Writing",
    // and a whole file (a prototype's source) in a few seconds, not a minute.
    const chunk = Math.max(24, Math.ceil(json.length / 40));
    for (let i = 0; i < json.length; i += chunk) this.emit({ type: "tool-input-delta", id: toolCallId, delta: json.slice(i, i + chunk) }, 90);
    this.emit({ type: "tool-input-end", id: toolCallId }, 60);
    this.emit({ type: "tool-call", toolCallId, toolName, input }, 30);
    const output = { ok: true, op, path, status: "applied" };
    this.emit({ type: "tool-result", toolCallId, toolName, input, output }, 250);
    this.parts.push({ type: "tool-call", toolCallId, toolName, input });
    this.results.push({ type: "tool-result", toolCallId, toolName, output: { type: "json", value: output } });
    this.lastWasText = false;
    return this;
  }

  end(): { frames: ScriptFrame[]; reply: ConversationMessage[] } {
    this.emit({ type: "turn-committed" }, 150);
    const reply: ConversationMessage[] = [{ role: "assistant", content: this.parts }];
    if (this.results.length > 0) reply.push({ role: "tool", content: this.results });
    return { frames: this.frames, reply };
  }
}

/** The kickoff's batch: the product map to confirm, and what the brief leaves open. */
export const KICKOFF_QUESTIONS: AskQuestionInput[] = [
  {
    question:
      "Here is the product map I'd propose: actors Employee, Manager and Finance; features Submit expenses, Approvals " +
      "and Payroll export. Does this match what you have in mind?",
    options: [
      {
        label: "Matches: proceed with these actors and features",
        description: "I'll write the product page with these three actors and three features.",
        recommended: true,
      },
      {
        label: "Something is missing or should be split",
        description: "Type what to add, remove, rename or split, such as a separate Notifications feature.",
        freeText: true,
      },
    ],
  },
  {
    question: "Should an AI agent read uploaded receipts and pre-fill the expense claim for the employee to review?",
    detail: "Optional work an agent could do in Submit expenses. If you decline, employees enter every field by hand.",
    options: [
      {
        label: "Yes, suggest fields from the receipt",
        description: "The agent proposes amount, date, merchant and category; the employee can correct them.",
        recommended: true,
      },
      { label: "No, manual entry only", description: "Simpler to build; no agent component in the design." },
    ],
  },
  {
    question: "How are people told that a claim needs them, or that its status changed?",
    detail: "This decides whether Notifications is its own feature or a product-wide rule.",
    options: [
      { label: "In-app only", description: "A badge and a list in the app; no email.", recommended: true },
      { label: "In-app and email", description: "Adds an email channel the design has to provision." },
    ],
  },
  {
    question: "Which expense categories does a claim use?",
    detail: "Pick every category the first release needs.",
    options: [
      { label: "Travel" },
      { label: "Meals" },
      { label: "Accommodation" },
      { label: "Equipment" },
    ],
    multiSelect: true,
  },
];

/** A feature's two questions, and what the answers become in its file. */
interface FeatureInterview {
  questions: [AskQuestionInput, AskQuestionInput];
  write: (answers: [Answer, Answer]) => { stories: string[]; decisions: string[] };
  /** The one thing the agent assumed, written with its `*assumed*` tag. */
  assumed: string;
}

/** An answer read back: the option chosen (if any) and the words typed. */
interface Answer {
  option: string | null;
  text: string;
}

function lowerFirst(text: string): string {
  return text.charAt(0).toLowerCase() + text.slice(1);
}

function sentence(text: string): string {
  const t = text.trim();
  return /[.!?]$/.test(t) ? t : `${t}.`;
}

const SPENDING_REPORTS: FeatureInterview = {
  questions: [
    {
      question: "Who reads the spending reports?",
      detail: "This decides whose spending a report can show, and whether managers get a view of their own.",
      options: [
        { label: "Finance only", description: "One company-wide view, for finance." },
        {
          label: "Finance, and managers for their team",
          description: "Finance sees everything; each manager sees their own team's spending.",
          recommended: true,
        },
        { label: "Everyone, for their own spending", description: "Each employee also sees what they have claimed." },
      ],
    },
    {
      question: "Which breakdown does finance need first?",
      detail: "Other breakdowns can follow once the first one is in use.",
      options: [
        { label: "By team and category, per month", recommended: true },
        { label: "By project or client" },
      ],
    },
  ],
  write: ([who, breakdown]) => {
    const stories = ["As finance, I see approved spending broken down as the decisions below set out."];
    if (who.option === "Finance, and managers for their team") {
      stories.push("As a manager, I see my own team's spending the same way.");
    } else if (who.option === "Everyone, for their own spending") {
      stories.push("As an employee, I see what I have claimed this year.");
    }
    stories.push("As finance, I download any report as a spreadsheet.");
    const readers: Record<string, string> = {
      "Finance only": "Only finance reads spending reports.",
      "Finance, and managers for their team": "Finance reads every report; a manager reads only their own team's.",
      "Everyone, for their own spending": "Finance reads every report; everyone else reads only their own spending.",
    };
    const breakdowns: Record<string, string> = {
      "By team and category, per month": "Reports break spending down by team and category, per month.",
      "By project or client": "Reports break spending down by project or client.",
    };
    return {
      stories,
      decisions: [
        (who.option && readers[who.option]) || sentence(`Reports are read by ${lowerFirst(who.text)}`),
        (breakdown.option && breakdowns[breakdown.option]) || sentence(`The first breakdown is ${lowerFirst(breakdown.text)}`),
      ],
    };
  },
  assumed: "A report counts approved claims only, as of the last payroll export.",
};

const MILEAGE_CLAIMS: FeatureInterview = {
  questions: [
    {
      question: "How is a mileage claim priced?",
      options: [
        { label: "The company's rate per mile", description: "One rate for every car; finance sets it.", recommended: true },
        { label: "Actual fuel receipts", description: "Staff attach their fuel receipts instead of a rate." },
      ],
    },
    {
      question: "How does an employee give the route?",
      options: [
        { label: "Start and end address", description: "The distance is worked out for them.", recommended: true },
        { label: "They type the distance" },
      ],
    },
  ],
  write: ([pricing, route]) => ({
    stories: [
      route.option === "Start and end address"
        ? "As an employee, I enter a trip's start and end address, and the distance is worked out for me."
        : route.option === "They type the distance"
          ? "As an employee, I enter the distance of a trip myself."
          : sentence(`As an employee, I record a trip: ${lowerFirst(route.text)}`),
    ],
    decisions: [
      pricing.option === "The company's rate per mile"
        ? "Mileage is paid at one company rate per mile, which finance sets."
        : pricing.option === "Actual fuel receipts"
          ? "Mileage is paid from fuel receipts, not a rate."
          : sentence(`Mileage is priced ${lowerFirst(pricing.text)}`),
    ],
  }),
  assumed: "A round trip counts as two trips.",
};

function genericInterview(name: string): FeatureInterview {
  return {
    questions: [
      {
        question: `Who uses ${name} most?`,
        options: [{ label: "Employees", recommended: true }, { label: "Managers" }, { label: "Finance" }],
      },
      {
        question: `What must ${name} get right first?`,
        options: [{ label: "Being quick to use", recommended: true }, { label: "Being hard to get wrong" }],
      },
    ],
    write: ([who, first]) => ({
      stories: [],
      decisions: [
        sentence(`${who.option ?? who.text} are the main users of ${name}`),
        sentence(`${name} must first be ${lowerFirst((first.option ?? first.text).replace(/^Being /, ""))}`),
      ],
    }),
    assumed: `${name} needs no setup before its first use.`,
  };
}

function interviewFor(feature: SpecFeature): FeatureInterview {
  if (feature.name === "Spending reports") return SPENDING_REPORTS;
  if (feature.name === "Mileage claims") return MILEAGE_CLAIMS;
  return genericInterview(feature.name);
}

/** The answer in a message: the card's serialized answer, or the words of a typed reply. */
function readAnswer(instruction: string, question: AskQuestionInput): Answer {
  let body = instruction.trim();
  if (body.startsWith(ANSWER_PREFIX)) body = body.slice(body.indexOf('": ') + 3);
  else if (body.startsWith(ANSWERS_PREFIX)) body = body.split("\n")[1]?.replace(/^- "[^"]*": /, "") ?? "";
  const option = question.options.find((o) => body.startsWith(o.label));
  if (!option) return { option: null, text: body };
  const note = body.slice(option.label.length).replace(/^ — /, "").trim();
  return { option: option.label, text: note || option.label };
}

/** The file with the interview's stories and decisions added: the edit, and the file after it. */
function writeUp(feature: SpecFeature, current: string, added: { stories: string[]; decisions: string[] }, assumed: string) {
  const body = current.trimEnd();
  const oldString = body.split("\n").at(-1)!;
  const numbers = [...body.matchAll(new RegExp(`\\b${feature.id}\\.(\\d+)`, "g"))].map((m) => Number(m[1]));
  let n = numbers.length > 0 ? Math.max(...numbers) : 0;
  const stories = added.stories.map((s) => `- ${feature.id}.${++n} ${s}`);
  const lastSection = body.split("\n").filter((l) => l.startsWith("## ")).at(-1);
  let addition = "";
  if (stories.length > 0) {
    addition += lastSection === "## User Stories" ? `\n${stories.join("\n")}` : `\n\n## User Stories\n\n${stories.join("\n")}`;
  }
  addition += `\n\n## Decisions\n\n${[...added.decisions.map((d) => `- ${d}`), `- ${assumed} *assumed*`].join("\n")}`;
  const newString = oldString + addition;
  return { oldString, newString, content: `${body}${addition}\n`, stories: stories.length, decisions: added.decisions.length + 1 };
}

export interface TurnRequest {
  instruction: string;
  scope: WireScope;
  model: MockSpecModel;
  /** Every spec file's lines as the user has them now (the local doc stands in for the room). */
  lines: ReadonlyMap<string, LineBlock[]>;
  progress: InterviewProgress | undefined;
  /** The project's brief, for the kickoff. */
  prompt: string | undefined;
  /** Tells this turn's tool calls apart from every other's. */
  turnKey: string;
}

export interface ScriptedTurn {
  /** The display record: the message as the transcript shows it. */
  display: string;
  frames: ScriptFrame[];
  reply: ConversationMessage[];
  effect?: InterviewEffect;
  /** Where the interview stands after this turn. */
  progress: InterviewProgress | undefined;
}


function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? "" : "s"}`;
}

/** What the mock agent does with a message. */
export function scriptTurn(req: TurnRequest): ScriptedTurn {
  const { instruction, scope, model, progress, turnKey } = req;
  const text = instruction.trim();
  const inScope = scope.kind === "feature" ? model.features.find((f) => f.id === scope.featureId) : undefined;

  // The kickoff: the platform's `/start`, with the brief attached as the server does.
  if (text === START_COMMAND || text.startsWith(`${START_COMMAND} `)) {
    const brief = text.slice(START_COMMAND.length).trim() || req.prompt || "";
    const s = new Script()
      .pause(600)
      .say("I've read your brief. A few questions before I propose the features it describes.")
      .askAll(`${turnKey}-kickoff`, KICKOFF_QUESTIONS);
    return { display: brief ? `${START_COMMAND} ${brief}` : START_COMMAND, ...s.end(), progress };
  }

  // Starting an interview: the feature it names, scoped to that feature.
  const asked = parseInterviewCommand(text)?.featureId;
  const named = asked ? model.features.find((f) => f.id === asked) : undefined;
  const feature = named && inScope && named.id === inScope.id ? named : undefined;
  if (feature) {
    const script = interviewFor(feature);
    const s = new Script()
      .pause(500)
      .say(`Two questions to pin ${feature.name} down, then I'll write it up.`)
      .ask(`${turnKey}-q1`, script.questions[0]);
    return {
      display: text,
      ...s.end(),
      effect: { featureId: feature.id, stage: "Interviewing" },
      progress: { featureId: feature.id, step: 1, answers: [] },
    };
  }

  // An answer (from the card, or typed) in the interview under way, from its feature.
  const interviewed = progress && inScope?.id === progress.featureId ? inScope : undefined;
  if (progress && interviewed) {
    const script = interviewFor(interviewed);
    if (progress.step === 1) {
      const s = new Script().pause(500).say("Got it. One more.").ask(`${turnKey}-q2`, script.questions[1]);
      return { display: text, ...s.end(), progress: { ...progress, step: 2, answers: [text] } };
    }
    const answers: [Answer, Answer] = [
      readAnswer(progress.answers[0] ?? "", script.questions[0]),
      readAnswer(text, script.questions[1]),
    ];
    const current = model.files[interviewed.path] ?? `# ${interviewed.name}\n`;
    const up = writeUp(interviewed, current, script.write(answers), script.assumed);
    const s = new Script()
      .pause(600)
      .say(`Thanks. Writing up ${interviewed.name}.`)
      .edit(`${turnKey}-w`, interviewed.path, up.oldString, up.newString)
      .say(
        `${interviewed.name} is written: ${up.stories === 1 ? "1 new story" : `${up.stories} new stories`} and ${plural(up.decisions, "decision")}. ` +
          "One line is my assumption. Keep it, remove it or edit it below, or on the page.",
      );
    return {
      display: text,
      ...s.end(),
      effect: { featureId: interviewed.id, stage: "Interviewed", file: { path: interviewed.path, content: up.content } },
      progress: undefined,
    };
  }

  // Anything else: a short answer, about what the scope is about.
  if (scope.kind === "product") {
    const interviewedCount = model.features.filter((f) => f.stage === "Interviewed" || f.stage === "Designed").length;
    const toInterview = model.features.filter((f) => f.stage === "Not interviewed").map((f) => f.name);
    const blocked = model.features.filter((f) => blockingQuestions(req.lines.get(f.path) ?? []).length > 0).map((f) => f.name);
    const lines = [
      model.features.length === 0
        ? "There are no features yet; I'll propose them from the brief."
        : `${interviewedCount} of ${plural(model.features.length, "feature")} are interviewed.`,
      ...(toInterview.length > 0 ? [`Still to interview: ${toInterview.join(", ")}.`] : []),
      ...(blocked.length > 0 ? [`${blocked.join(", ")} waits on a question for you.`] : []),
    ];
    return { display: text, ...new Script().pause(500).say(lines.join(" ")).end(), progress };
  }
  if (scope.kind === "design") {
    return {
      display: text,
      ...new Script().pause(500).say("Noted for the design review. It goes into the next design pass.").end(),
      progress,
    };
  }
  const about = inScope?.name ?? "this feature";
  return {
    display: text,
    ...new Script()
      .pause(500)
      .say(`Noted for ${about}. A change that reaches other features is made there too, and I'll say which.`)
      .end(),
    progress,
  };
}
