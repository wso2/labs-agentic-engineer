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

// The shell's one layout breakpoint, as prototyped: below it the chat becomes
// an overlay beside the rail, the track stacks, and a card's inset shrinks.
export const PHONE_QUERY = "(max-width: 860px)";

/** `sx` key for styles that apply at phone width. */
export const PHONE = `@media ${PHONE_QUERY}`;

export const RAIL_WIDTH = 52;
/** The chat's width as an overlay at phone width; beside the page it is `chatWidth`'s. */
export const CHAT_OVERLAY_WIDTH = 360;
