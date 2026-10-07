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

/**
 * The prototype the host shows. Preview: the frame runtime is fetched once
 * and revisions and findings arrive over the event stream (a revision with
 * findings never arrives, so the last good one stays). Export: all of it is
 * in the page.
 */

import { useEffect, useState } from "react";
import type { Finding } from "@wso2/prototype-kit/check";
import type { HostConfig, PrototypeRevision } from "../host-config.js";

export interface LivePrototype {
  runtime: string | null;
  revision: PrototypeRevision | null;
  findings: Finding[];
  /** The preview server could not be reached. */
  error: string | null;
}

export function useLivePrototype(config: HostConfig): LivePrototype {
  const [state, setState] = useState<LivePrototype>(() =>
    config.mode === "export"
      ? { runtime: config.frameRuntime, revision: config.revision, findings: [], error: null }
      : { runtime: null, revision: null, findings: [], error: null },
  );

  useEffect(() => {
    if (config.mode !== "preview") return;
    let live = true;
    fetch("frame-runtime.js")
      .then((r) => {
        if (!r.ok) throw new Error(`frame-runtime.js: HTTP ${r.status}`);
        return r.text();
      })
      .then((runtime) => live && setState((s) => ({ ...s, runtime })))
      .catch((e: unknown) => live && setState((s) => ({ ...s, error: e instanceof Error ? e.message : String(e) })));
    const events = new EventSource("events");
    events.addEventListener("update", (e) => {
      const revision = JSON.parse((e as MessageEvent<string>).data) as PrototypeRevision;
      setState((s) => (s.revision?.hash === revision.hash ? s : { ...s, revision }));
    });
    events.addEventListener("findings", (e) => {
      const { findings } = JSON.parse((e as MessageEvent<string>).data) as { findings: Finding[] };
      setState((s) => ({ ...s, findings }));
    });
    return () => {
      live = false;
      events.close();
    };
  }, [config]);

  return state;
}
