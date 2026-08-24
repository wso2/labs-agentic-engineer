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

import "@testing-library/jest-dom";
import { configure } from "@testing-library/react";

// Testing Library's default 1s async-util budget is a CI flake source, not a
// correctness bound: a component test's `waitFor` competes with every other
// worker on the runner, and a heavy MUI render (the spec view's drawers) can
// need well past a second on a loaded machine while passing instantly here.
// Raise the ceiling — a genuinely stuck expectation still fails, just later,
// and the per-test timeout below stays the real backstop.
configure({ asyncUtilTimeout: 5_000 });

// jsdom implements neither `URL.createObjectURL` nor `revokeObjectURL`, so any
// component that previews a File (the chat composer's image attachments, #428)
// throws on render without these. Stubbed globally rather than guarded in the
// component: the browser always has them, and a guard would mean shipping a
// branch that only ever runs in tests.
// Guarded independently: a partial implementation (one present, one missing)
// would skip a combined check and leave AttachmentPreview's cleanup calling
// undefined.
let objectUrlCounter = 0;
if (typeof URL.createObjectURL !== "function") {
  URL.createObjectURL = () => `blob:jsdom/${++objectUrlCounter}`;
}
if (typeof URL.revokeObjectURL !== "function") {
  URL.revokeObjectURL = () => {};
}
