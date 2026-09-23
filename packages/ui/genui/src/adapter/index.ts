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

// The swap point. Everything outside src/adapter/json-render/ reaches the
// renderer library only through this file, so replacing json-render (e.g. with
// an A2UI renderer) means a new sibling adapter directory and changing these
// two export lines — the catalog and every design system stay as they are.
export * from "./headless.js";
export {
  createGenUiView,
  type GenUiViewProps,
} from "./json-render/createGenUiView.js";
