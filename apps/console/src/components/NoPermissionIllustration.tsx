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

// An unlocked-padlock-with-denial-mark illustration for "you can't see this"
// states — reads as access revoked rather than a plain closed lock. Pure
// `currentColor` strokes, so it inherits EmptyState's icon-wrapper color
// (and its opacity treatment) in both themes without its own light/dark
// branching.
export function NoPermissionIllustration({ size = 96 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 96 96"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden="true"
    >
      <path
        d="M33 48 V34 a10 10 0 0 1 20 0 v6"
        stroke="currentColor"
        strokeWidth="3"
        strokeLinecap="round"
      />
      <rect x="26" y="48" width="34" height="26" rx="5" stroke="currentColor" strokeWidth="3" />
      <circle cx="40" cy="59" r="3.2" fill="currentColor" />
      <path d="M40 62.2 V67" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" />
      <circle cx="66" cy="66" r="13" stroke="currentColor" strokeWidth="3" />
      <path
        d="M61 61 L71 71 M71 61 L61 71"
        stroke="currentColor"
        strokeWidth="3"
        strokeLinecap="round"
      />
    </svg>
  );
}
