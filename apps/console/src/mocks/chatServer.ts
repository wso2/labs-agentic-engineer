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

import type { PrototypeFeedback } from "../features/agent-chat/turnScope";
import type { StreamPart } from "@aep/agent-stream";
import type { components } from "../generated/aep-api";
import type { FeatureStage } from "../features/spec/api/specModel";
import type { DesignEffect } from "./designState";

type ConversationMessage = components["schemas"]["ConversationMessage"];

// The mock's agent server: every turn a project's conversation has had, what
// each one streams and when, and what it leaves behind (its messages in the
// history, a feature's stage and file). A turn is scheduled, not simulated:
// it has a start time and a timed script, so whether it is still running, and
// which of its frames have happened, is worked out from the clock. That is
// what lets a reload land in the middle of a turn and attach to it again.
//
// Kept in sessionStorage, so a reload keeps the conversation and a new tab
// starts it over. The spec model's interview stages are read from here
// (mocks/specState.ts), so the stage, the file and the conversation agree
// after a reload; so is what the design turns did (mocks/designState.ts).

/** One frame of a turn's stream, `at` milliseconds after it started. */
export interface ScriptFrame {
  at: number;
  part: StreamPart;
}

/** What a finished turn changed in the spec: a feature's stage, and its file's new words. */
export interface InterviewEffect {
  featureId: string;
  stage: FeatureStage;
  /** Only once the turn has finished: the file as the turn left it. */
  file?: { path: string; content: string };
}

/** Where a project's interview is: which feature, and which question it waits on. */
export interface InterviewProgress {
  featureId: string;
  /** 1: waiting on the first question's answer; 2: on the second. */
  step: 1 | 2;
  /** The answers so far, in the user's words. */
  answers: string[];
}

export interface MockTurn {
  turnId: string;
  projectName: string;
  conversationId: string;
  /** The display record: the message that started it. */
  instruction: string;
  /** The scope it was sent with, which the history carries back as the platform's does. */
  scope?: components["schemas"]["TurnScope"];
  startedAt: number;
  frames: ScriptFrame[];
  /** Its messages, persisted to the history when it ends. */
  reply: ConversationMessage[];
  effect?: InterviewEffect;
  /** What a design-review turn does to the design, once it has finished (designState.ts). */
  design?: DesignEffect;
  /** The prototype files a `/prototype` turn left, by room path (fixtures/prototype.ts). */
  prototype?: Record<string, string>;
  /** The review batch a `/prototype` turn carried, journaled with its message as the platform does. */
  prototypeFeedback?: PrototypeFeedback;
  /** Why the turn failed (its stream ends `turn-failed`); absent when it completed. */
  failure?: string;
}

interface State {
  turns: MockTurn[];
  interviews: Record<string, InterviewProgress | undefined>;
}

const KEY = "aep:mock:chat-server";

function read(): State {
  try {
    const raw = sessionStorage.getItem(KEY);
    if (raw) return JSON.parse(raw) as State;
  } catch {
    // unreadable: start over
  }
  return { turns: [], interviews: {} };
}

function write(state: State): void {
  try {
    sessionStorage.setItem(KEY, JSON.stringify(state));
  } catch {
    /* quota: non-fatal in mock mode */
  }
}

export function conversationIdFor(projectName: string): string {
  return `conv-${projectName}`;
}

function duration(turn: MockTurn): number {
  return turn.frames.at(-1)?.at ?? 0;
}

export function isRunning(turn: MockTurn, now = Date.now()): boolean {
  return now < turn.startedAt + duration(turn);
}

export function runningTurn(projectName: string): MockTurn | undefined {
  return read().turns.find((t) => t.projectName === projectName && isRunning(t));
}

export function findTurn(turnId: string): MockTurn | undefined {
  return read().turns.find((t) => t.turnId === turnId);
}

/** Every turn a project has had, running or not, oldest first. */
export function projectTurns(projectName: string): MockTurn[] {
  return read().turns.filter((t) => t.projectName === projectName);
}

/** A conversation's finished turns, oldest first. */
export function finishedTurns(conversationId: string): MockTurn[] {
  return read().turns.filter((t) => t.conversationId === conversationId && !isRunning(t));
}

let counter = 0;

/** Schedule a turn from now. */
export function recordTurn(turn: Omit<MockTurn, "turnId" | "startedAt">): MockTurn {
  const state = read();
  counter += 1;
  const recorded: MockTurn = { ...turn, turnId: `mock-turn-${Date.now().toString(36)}-${counter}`, startedAt: Date.now() };
  state.turns.push(recorded);
  write(state);
  return recorded;
}

export function interviewProgress(projectName: string): InterviewProgress | undefined {
  return read().interviews[projectName];
}

export function setInterviewProgress(projectName: string, progress: InterviewProgress | undefined): void {
  const state = read();
  state.interviews[projectName] = progress;
  write(state);
}

/**
 * Each feature's interview stage and file, as the project's turns have left
 * them: an interview is under way from its first turn's start, and its file
 * is written once the turn that writes it has finished.
 */
export function interviewEffects(projectName: string, now = Date.now()): Map<string, InterviewEffect> {
  const effects = new Map<string, InterviewEffect>();
  for (const turn of read().turns) {
    if (turn.projectName !== projectName || !turn.effect) continue;
    if (turn.effect.file && isRunning(turn, now)) continue;
    effects.set(turn.effect.featureId, turn.effect);
  }
  return effects;
}

/**
 * The prototype files finished `/prototype` turns wrote, the latest of each:
 * the local doc is seeded with them after a reload, as the room would hold them.
 */
export function prototypeFileWrites(projectName: string, now = Date.now()): Map<string, string> {
  const files = new Map<string, string>();
  for (const turn of read().turns) {
    if (turn.projectName !== projectName || !turn.prototype || isRunning(turn, now)) continue;
    for (const [path, content] of Object.entries(turn.prototype)) files.set(path, content);
  }
  return files;
}
