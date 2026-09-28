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

import { describe, expect, it, vi } from "vitest";
import { runFixLoop } from "../src/loop.js";
import type { Verdict } from "../src/verdict.js";

const verdict = (overall: number): Verdict => ({
  passed: overall >= 0.8,
  overall,
  scenarios: [{ id: "SC-1", score: overall, passed: overall >= 0.8, failed: [], ungraded: [] }],
});

describe("runFixLoop", () => {
  it("stops as soon as the evaluation passes", async () => {
    const evaluate = vi.fn().mockResolvedValue(verdict(0.9));
    const revise = vi.fn();
    const r = await runFixLoop({ evaluate, revise, body: "original" });
    expect(r.rounds).toBe(1);
    expect(revise).not.toHaveBeenCalled();
    expect(r.body).toBe("original");
  });

  // Unbounded iteration on a probabilistic system spends the org's key with no
  // guarantee of converging.
  it("never exceeds the cap", async () => {
    const evaluate = vi.fn().mockResolvedValue(verdict(0.2));
    const revise = vi.fn(async (_v, b: string) => `${b}+`);
    const r = await runFixLoop({ evaluate, revise, body: "x", maxRounds: 3 });
    expect(evaluate).toHaveBeenCalledTimes(3);
    expect(r.rounds).toBe(3);
  });

  // Pins M5: dropping the `round === max` break still leaves the round count
  // right, but fires one extra `revise` whose output is never evaluated — a
  // paid model call spent for nothing.
  it("does not revise after the final evaluation", async () => {
    const evaluate = vi.fn().mockResolvedValue(verdict(0.2));
    const revise = vi.fn(async (_v, b: string) => `${b}+`);
    await runFixLoop({ evaluate, revise, body: "x", maxRounds: 3 });
    expect(revise).toHaveBeenCalledTimes(2);
  });

  // The loop can talk itself into a worse agent; the best prompt must ship.
  it("keeps the best-scoring body, not the last one tried", async () => {
    const evaluate = vi
      .fn()
      .mockResolvedValueOnce(verdict(0.5))
      .mockResolvedValueOnce(verdict(0.7))
      .mockResolvedValueOnce(verdict(0.3));
    const revise = vi.fn(async (_v, b: string) => `${b}+`);
    const r = await runFixLoop({ evaluate, revise, body: "v0", maxRounds: 3 });
    expect(r.body).toBe("v0+");
    expect(r.best.overall).toBe(0.7);
  });

  // Pins M4: a plateau (equal score) is no evidence a revision helped, so the
  // strict `>` must keep the EARLIEST body that reached that score — the
  // cheaper prompt, not a later one that only tied it.
  it("keeps the earliest body on a tie, not a later one that only matched it", async () => {
    const evaluate = vi
      .fn()
      .mockResolvedValueOnce(verdict(0.5))
      .mockResolvedValueOnce(verdict(0.5));
    const revise = vi.fn(async (_v, b: string) => `${b}+`);
    const r = await runFixLoop({ evaluate, revise, body: "v0", maxRounds: 2 });
    expect(r.body).toBe("v0");
    expect(r.best.overall).toBe(0.5);
  });

  it("stops early when a round scores worse than the one before", async () => {
    const evaluate = vi
      .fn()
      .mockResolvedValueOnce(verdict(0.6))
      .mockResolvedValueOnce(verdict(0.4));
    const revise = vi.fn(async (_v, b: string) => `${b}+`);
    const r = await runFixLoop({ evaluate, revise, body: "v0", maxRounds: 3 });
    expect(evaluate).toHaveBeenCalledTimes(2);
    expect(r.body).toBe("v0");
  });

  // Not vacuous: a first-round pass must ship a REAL verdict as `best`, not a
  // stand-in that would let a caller mistake "never evaluated" for "passed".
  it("reports a real verdict as best when the first round already passes", async () => {
    const evaluate = vi.fn().mockResolvedValue(verdict(0.95));
    const revise = vi.fn();
    const r = await runFixLoop({ evaluate, revise, body: "original" });
    expect(r.best).toEqual(verdict(0.95));
    expect(r.history).toEqual([verdict(0.95)]);
  });

  describe("maxRounds validation", () => {
    // A cap below 1 would let the loop return without ever calling
    // `evaluate` — `best` would have nothing real to report. That is a
    // caller error, not a silent vacuous pass, so it must fail loudly
    // instead of returning a fabricated or null verdict.
    it("rejects a cap that would produce zero rounds", async () => {
      const evaluate = vi.fn();
      const revise = vi.fn();
      await expect(runFixLoop({ evaluate, revise, body: "x", maxRounds: 0 })).rejects.toThrow(
        /maxRounds/,
      );
      expect(evaluate).not.toHaveBeenCalled();
    });

    // `maxRounds` may only ever LOWER the hard cap of 3, never raise it — a
    // caller asking for more has misunderstood the contract, and silently
    // clamping to 3 would hide that misunderstanding instead of surfacing it.
    it("rejects a cap above the hard iteration cap of 3", async () => {
      const evaluate = vi.fn();
      const revise = vi.fn();
      await expect(runFixLoop({ evaluate, revise, body: "x", maxRounds: 10 })).rejects.toThrow(
        /maxRounds/,
      );
      expect(evaluate).not.toHaveBeenCalled();
    });

    // Keeps the hard cap of 3 when the caller supplies no override at all.
    it("keeps 3 as the ceiling when maxRounds is omitted", async () => {
      const evaluate = vi.fn().mockResolvedValue(verdict(0.2));
      const revise = vi.fn(async (_v, b: string) => `${b}+`);
      const r = await runFixLoop({ evaluate, revise, body: "x" });
      expect(evaluate).toHaveBeenCalledTimes(3);
      expect(r.rounds).toBe(3);
    });

    // `NaN < 1` and `1 <= NaN` are both false, so a naive lower-bound guard
    // is bypassed entirely and the loop body never runs — the same
    // pass-on-empty-input shape that cost two earlier fix rounds in this
    // plan. `Infinity` and fractional caps are the same family of defect:
    // none of them is a valid integer round count.
    it.each([NaN, Infinity, -Infinity, 2.5, -1])(
      "rejects a non-integer or out-of-range maxRounds (%s)",
      async (bad) => {
        const evaluate = vi.fn();
        const revise = vi.fn();
        await expect(
          runFixLoop({ evaluate, revise, body: "x", maxRounds: bad }),
        ).rejects.toThrow(/maxRounds/);
        expect(evaluate).not.toHaveBeenCalled();
      },
    );
  });
});
