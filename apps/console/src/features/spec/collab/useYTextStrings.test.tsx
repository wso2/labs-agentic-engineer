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

// @vitest-environment jsdom

import { act, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import * as Y from "yjs";
import { useYTextStrings } from "./useYTextStrings";

function Probe({ texts }: { texts: readonly (Y.Text | null)[] }) {
  const values = useYTextStrings(texts);
  return <div data-testid="out">{values.map((v) => v ?? "<null>").join("|")}</div>;
}

function docWith(...contents: string[]): { doc: Y.Doc; texts: Y.Text[] } {
  const doc = new Y.Doc();
  const texts = contents.map((c, i) => {
    const t = doc.getText(`t${i}`);
    t.insert(0, c);
    return t;
  });
  return { doc, texts };
}

const out = () => screen.getByTestId("out").textContent;

describe("useYTextStrings", () => {
  it("returns every document's content, in the order given", () => {
    const { texts } = docWith("one", "two");
    render(<Probe texts={texts} />);
    expect(out()).toBe("one|two");
  });

  it("re-renders when any one of them changes", () => {
    const { doc, texts } = docWith("one", "two");
    render(<Probe texts={texts} />);

    act(() => {
      doc.transact(() => texts[1]!.insert(3, " more"));
    });
    expect(out()).toBe("one|two more");

    act(() => {
      doc.transact(() => texts[0]!.insert(3, "!"));
    });
    expect(out()).toBe("one!|two more");
  });

  it("carries a hole for a document the room has not delivered", () => {
    const { texts } = docWith("one");
    render(<Probe texts={[texts[0]!, null]} />);
    expect(out()).toBe("one|<null>");
  });

  it("handles being given nothing at all", () => {
    render(<Probe texts={[]} />);
    expect(out()).toBe("");
  });

  // THE trap this hook exists to avoid. `texts.map(t => t.toString())` builds a
  // fresh array on every call, React compares snapshots with Object.is, and the
  // result is an immediate render loop rather than a subtle bug. If the cache
  // regressed, rendering would blow the stack before reaching the assertion.
  it("returns the identical array while nothing has changed", () => {
    const { doc, texts } = docWith("one", "two");
    const seen: (readonly (string | null)[])[] = [];
    function Collect() {
      const values = useYTextStrings(texts);
      seen.push(values);
      return <div data-testid="out">{values.join("|")}</div>;
    }
    const { rerender } = render(<Collect />);
    rerender(<Collect />);
    rerender(<Collect />);

    expect(seen.length).toBeGreaterThanOrEqual(3);
    for (const snapshot of seen) expect(snapshot).toBe(seen[0]);

    // And a real change still produces a new one.
    act(() => {
      doc.transact(() => texts[0]!.insert(3, "!"));
    });
    expect(seen[seen.length - 1]).not.toBe(seen[0]);
  });

  it("stops observing when it goes away", () => {
    const { doc, texts } = docWith("one");
    const { unmount } = render(<Probe texts={texts} />);
    unmount();
    // Writing after unmount must not reach a torn-down component: React logs a
    // warning for a setState on an unmounted tree, and the test-setup treats a
    // console error as a failure.
    act(() => {
      doc.transact(() => texts[0]!.insert(3, " after"));
    });
    expect(texts[0]!.toString()).toBe("one after");
  });
});
