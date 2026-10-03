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

import { useEffect, useMemo, useState } from "react";
import { prototypeHash } from "@wso2/prototype-kit/feedback";

// What the review loads besides the prototype: the Oxygen theme's frame
// runtime, the script the sandboxed frame runs the prototype with (about
// 2 MB, so it is fetched when a review first opens, not with the app), and
// the revision's hash, which names what feedback was given on.

type Loaded<T> = { value: T; error: null } | { value: null; error: string | null };

let runtime: Promise<string> | null = null;

function loadRuntime(): Promise<string> {
  // A failed load is tried again on the next review.
  runtime ??= import("@wso2/prototype-theme-oxygen/frame-runtime.js?raw").then(
    (m: { default: string }) => m.default,
    (e: unknown) => {
      runtime = null;
      throw e;
    },
  );
  return runtime;
}

/** The Oxygen frame runtime's text; null while it loads, with the error when it could not be. */
export function useFrameRuntime(): Loaded<string> {
  const [state, setState] = useState<Loaded<string>>({ value: null, error: null });
  useEffect(() => {
    let live = true;
    loadRuntime().then(
      (value) => live && setState({ value, error: null }),
      (e: unknown) => live && setState({ value: null, error: e instanceof Error ? e.message : String(e) }),
    );
    return () => {
      live = false;
    };
  }, []);
  return state;
}

/**
 * The revision hash of these files: the kit's, computed in plain JavaScript,
 * so it is the same over plain HTTP, where Web Crypto's `crypto.subtle` is
 * missing, as over HTTPS.
 */
export function usePrototypeHash(manifestText: string, source: string): string {
  return useMemo(() => prototypeHash(manifestText, source), [manifestText, source]);
}
