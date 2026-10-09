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
/** The frame's geometry watch stops cleanly: no measure left pending to report after it. */

import { afterEach, describe, expect, it, vi } from "vitest";
import { watchGeometry } from "../src/runtime/geometry.js";

class Observer {
  observe() {}
  disconnect() {}
}

function stubFrameDocument() {
  const frames = new Map<number, () => void>();
  let next = 1;
  vi.stubGlobal("window", { addEventListener() {}, removeEventListener() {} });
  vi.stubGlobal("document", { documentElement: {}, body: {}, querySelector: () => null });
  vi.stubGlobal("ResizeObserver", Observer);
  vi.stubGlobal("MutationObserver", Observer);
  vi.stubGlobal("requestAnimationFrame", (fn: () => void) => {
    frames.set(next, fn);
    return next++;
  });
  vi.stubGlobal("cancelAnimationFrame", (id: number) => frames.delete(id));
  return { runFrames: () => [...frames.values()].forEach((fn) => fn()), pending: () => frames.size };
}

afterEach(() => vi.unstubAllGlobals());

describe("watchGeometry", () => {
  it("measures at most once a frame", () => {
    const frames = stubFrameDocument();
    const report = vi.fn();
    const watch = watchGeometry(() => [], report);
    watch.refresh();
    watch.refresh();
    expect(frames.pending()).toBe(1);
    frames.runFrames();
    expect(report).toHaveBeenCalledTimes(1);
  });

  it("cancels a pending measure when stopped, so nothing reports after", () => {
    const frames = stubFrameDocument();
    const report = vi.fn();
    const watch = watchGeometry(() => [], report);
    watch.refresh();
    watch.stop();
    expect(frames.pending()).toBe(0);
    frames.runFrames();
    expect(report).not.toHaveBeenCalled();
  });
});
