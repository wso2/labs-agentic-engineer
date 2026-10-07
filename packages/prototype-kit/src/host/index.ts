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
 * `@wso2/prototype-kit/host`: the browser side a host page needs — the
 * sandboxed `PrototypeFrame`, the bridge's messages and parsers, the frame
 * document and its CSP, and the pure view-state reducer. No theme components.
 */

export {
  parseFromFrameMessage,
  parseToFrameMessage,
  type FrameColorScheme,
  type FrameElement,
  type FrameMode,
  type FrameView,
  type FromFrameMessage,
  type ToFrameMessage,
} from "./bridge.js";
export { PROTOTYPE_FRAME_CSP, prototypeFrameDocument } from "./frame-document.js";
export { PROTOTYPE_START_TIMEOUT_MS, PrototypeFrame, type PrototypeFrameProps } from "./PrototypeFrame.js";
export { PrototypeWindow, prototypeAddress, type PrototypeWindowProps } from "./PrototypeWindow.js";
export {
  frameViewOf,
  initialPrototypeView,
  reducePrototypeView,
  type PrototypeMode,
  type PrototypeViewEvent,
  type PrototypeViewRequest,
  type PrototypeViewState,
} from "./view-state.js";
export { isDataSnapshot, type DataSnapshot } from "../data.js";
export type { PrototypeManifest } from "../manifest/types.js";
export { screensForRole } from "../manifest/screens.js";
