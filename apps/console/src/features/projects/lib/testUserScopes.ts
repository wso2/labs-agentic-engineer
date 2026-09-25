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
 * How a test user's scopes are named and ordered when they are put on screen.
 *
 * The scopes are detail on demand, not something a reader scans ACROSS rows —
 * the Role column is what an account is picked by. So the table cell holds a
 * count and a way in, and the list itself lives in a dialog. That keeps a row
 * a row whether the account carries six scopes or sixty.
 */

/** `resource:verb` grouped by resource, and alphabetical inside each — a
 *  reader looks for "what can this account do to food-entries", and the wire's
 *  order is the identity provider's, which means nothing here. */
export function displayScopes(scopes: readonly string[]): string[] {
  return [...scopes].sort((a, b) => a.localeCompare(b));
}

/** The cell's button: the column already says "Scopes", but the button must
 *  still read as one out of context (a screen reader's control list). */
export function scopesButtonText(count: number): string {
  return `Scopes · ${count}`;
}

/** The control's accessible name — the action, the count, and whose. */
export function scopesButtonLabel(count: number, username: string): string {
  return `Show ${count} scope${count === 1 ? "" : "s"} for ${username}`;
}
