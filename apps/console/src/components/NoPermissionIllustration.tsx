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

// A shield-and-lock illustration for "you can't see this" states — one step
// up from a bare Lock glyph (EmptyState's usual icon slot) where the denial
// is the whole point of the view, not a footnote on it. Pure `currentColor`
// strokes/fills, so it inherits EmptyState's icon-wrapper color (and its
// opacity treatment) in both themes without its own light/dark branching.
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
        d="M48 8 L80 20 V46 C80 66 66 80 48 88 C30 80 16 66 16 46 V20 Z"
        stroke="currentColor"
        strokeWidth="3"
        strokeLinejoin="round"
      />
      <path
        d="M40 46 V38 a8 8 0 0 1 16 0 V46"
        stroke="currentColor"
        strokeWidth="3"
        strokeLinecap="round"
      />
      <rect
        x="34"
        y="46"
        width="28"
        height="20"
        rx="3"
        stroke="currentColor"
        strokeWidth="3"
      />
      <circle cx="48" cy="54" r="2.5" fill="currentColor" />
      <path d="M48 56.5 V60" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
      <circle cx="20" cy="14" r="2" fill="currentColor" opacity="0.4" />
      <circle cx="76" cy="16" r="1.5" fill="currentColor" opacity="0.3" />
      <circle cx="78" cy="68" r="2.5" fill="currentColor" opacity="0.3" />
    </svg>
  );
}
