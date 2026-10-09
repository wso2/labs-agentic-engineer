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

import { FileBundle, type OpResult } from "@aep/agent-stream";
import type { RoomPeer } from "./room-peer.js";

/**
 * A FileBundle that mirrors every APPLIED mutation onto the live collab doc
 * (#86 phase 4). The bundle stays the LLM's plain-text working state (all
 * validation — anchored edits, YAML gates, write-guards — runs in the
 * superclass); only ops that actually changed content reach the doc, where
 * @aep/collab-doc applies them safely (Y.Text diff-and-patch / md fragment
 * reparse) as one agent-origin transaction per op — live typing for every
 * peer in the room.
 */
export class DocFileBundle extends FileBundle {
  constructor(
    private readonly peer: RoomPeer,
    initial: Record<string, string>,
  ) {
    super(initial);
  }

  override addFile(path: string, content: string): OpResult {
    // A newly-created file is accept-by-default — mirror it UNMARKED.
    return this.mirror(super.addFile(path, content), path, false);
  }

  override editFile(path: string, oldString: string, newString: string): OpResult {
    // An edit to already-committed content is reviewable — mirror it MARKED.
    return this.mirror(super.editFile(path, oldString, newString), path, true);
  }

  override removeFile(path: string): OpResult {
    const res = super.removeFile(path);
    if (res.ok && res.status === "applied") this.peer.delete(path);
    return res;
  }

  private mirror(res: OpResult, path: string, mark: boolean): OpResult {
    if (res.ok && res.status === "applied") {
      const content = this.read(path);
      if (content !== undefined) this.peer.set(path, content, mark);
    }
    return res;
  }
}
