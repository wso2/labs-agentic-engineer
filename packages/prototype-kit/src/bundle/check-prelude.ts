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
 * The few host APIs React's server renderer touches at load, for the bare V8
 * context the render check runs in (a vm context has JavaScript's built-ins
 * and nothing else). Timers never fire: rendering to a string is synchronous.
 * Must be the check runtime's first import, so it runs before React loads.
 */

const g = globalThis as Record<string, unknown>;

let nextTimer = 1;
const timer = () => nextTimer++;
for (const name of ["setTimeout", "setInterval", "setImmediate"]) g[name] ??= timer;
for (const name of ["clearTimeout", "clearInterval", "clearImmediate"]) g[name] ??= () => {};
g["queueMicrotask"] ??= (fn: () => void) => void Promise.resolve().then(fn);

/** A MessageChannel whose messages go nowhere: React's scheduler only checks it exists. */
class InertMessageChannel {
  port1 = { onmessage: null as unknown, postMessage() {}, close() {} };
  port2 = { onmessage: null as unknown, postMessage() {}, close() {} };
}
g["MessageChannel"] ??= InertMessageChannel;

/** UTF-8 encoding, for the server renderer's byte chunks. */
class Utf8Encoder {
  readonly encoding = "utf-8";
  encode(input = ""): Uint8Array {
    const bytes: number[] = [];
    for (const ch of input) {
      const c = ch.codePointAt(0)!;
      if (c < 0x80) bytes.push(c);
      else if (c < 0x800) bytes.push(0xc0 | (c >> 6), 0x80 | (c & 63));
      else if (c < 0x10000) bytes.push(0xe0 | (c >> 12), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
      else bytes.push(0xf0 | (c >> 18), 0x80 | ((c >> 12) & 63), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
    }
    return new Uint8Array(bytes);
  }
  encodeInto(input: string, dest: Uint8Array) {
    const bytes = this.encode(input);
    dest.set(bytes.subarray(0, dest.length));
    return { read: input.length, written: Math.min(bytes.length, dest.length) };
  }
}
g["TextEncoder"] ??= Utf8Encoder;

export {};
