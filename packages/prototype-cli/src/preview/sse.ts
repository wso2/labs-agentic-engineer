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

/** Server-sent events to every connected host page. */

import type { IncomingMessage, ServerResponse } from "node:http";

const HEARTBEAT_MS = 15_000;

export interface ServerEvent {
  event: string;
  data: unknown;
}

function frame({ event, data }: ServerEvent): string {
  return `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`;
}

export class EventStream {
  private readonly clients = new Set<ServerResponse>();
  private readonly heartbeat = setInterval(() => {
    for (const res of this.clients) res.write(": keep-alive\n\n");
  }, HEARTBEAT_MS);

  /** Hold `res` open as an event stream, starting with `initial`. */
  attach(req: IncomingMessage, res: ServerResponse, initial: readonly ServerEvent[]): void {
    res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-store", connection: "keep-alive" });
    for (const event of initial) res.write(frame(event));
    this.clients.add(res);
    req.on("close", () => this.clients.delete(res));
  }

  send(event: ServerEvent): void {
    for (const res of this.clients) res.write(frame(event));
  }

  close(): void {
    clearInterval(this.heartbeat);
    for (const res of this.clients) res.end();
    this.clients.clear();
  }
}
