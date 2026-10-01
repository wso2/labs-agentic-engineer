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

// Room IDs are `spec-<org>-<project>` (both DNS-label slugs). Because slugs
// may themselves contain hyphens the ID is ambiguous to split — only the BFF
// can, using the org recovered from the caller's token. This module therefore
// only *shape*-validates; the room is forwarded whole to the oracle.

const ROOM_PATTERN = /^spec-[a-z0-9]([a-z0-9-]*[a-z0-9])?$/;
const MAX_ROOM_LENGTH = 128;

export function isSpecRoom(name: string): boolean {
  return (
    name.length <= MAX_ROOM_LENGTH &&
    ROOM_PATTERN.test(name) &&
    // At least org + project after the prefix: one more hyphen required.
    name.slice("spec-".length).includes("-")
  );
}
