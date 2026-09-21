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

// Mock mode — copied verbatim to <app-path>/mock/badge.ts, never edited.
//
// WHO AM I, and switch. A small fixed control in the corner naming the role the
// mock session is signed in as, with every role in the design behind it.
//
// Switching a role is already a navigation — `?role=<name>` is the whole
// mechanism (see mock/authz/session.ts) — so this adds no concept: it is the
// address bar, in the page, for somebody who is clicking rather than typing.
// That matters most in WIRED mode, where a person walks real data through every
// role in one sitting and retyping a query string between each one is the
// friction that makes them stop after the first.
//
// Plain DOM, no React, no import of the app's own tree: mock/browser.ts is a
// `.ts` file copied to every app, auth dependency or not, and a component would
// drag JSX and the app's providers into it for a control the app must never
// see. Nothing under src/ changes, and nothing here reaches a production
// bundle — browser.ts is imported only under `import.meta.env.DEV && MODE ===
// "mock"`.

/** The value standing for "nobody is signed in" — not a role, so not a role name. */
const SIGNED_OUT = "aep-signed-out";

/** The key mock/authz/session.ts persists the last explicit `?role=` under. */
const ROLE_STORAGE_KEY = "aep-mock-role";

/** The roles the design declares, put on the page by mock/plugin.ts. */
const roles: string[] | null =
  (globalThis as { __AEP_MOCK_ROLES__?: string[] | null }).__AEP_MOCK_ROLES__ ?? null;

/** Whether the API behind this page is the real service (mock/plugin.ts again). */
const wired: boolean = (globalThis as { __AEP_WIRED__?: boolean }).__AEP_WIRED__ === true;

function storedRole(): string | null {
  try {
    return sessionStorage.getItem(ROLE_STORAGE_KEY);
  } catch {
    return null; // private mode: the URL is the only state there is
  }
}

/**
 * Who the app believes is signed in, read the way mock/authz/session.ts reads
 * it: the URL wins, the persisted role answers an internal navigation, and
 * neither means the first declared role.
 */
function activeValue(): string {
  const params = new URLSearchParams(window.location.search);
  if (params.get("auth") === "out") return SIGNED_OUT;
  const role = params.get("role") ?? storedRole();
  if (role === null) return roles?.[0] ?? "";
  return role;
}

/** Become somebody else: a full navigation, exactly as retyping the URL would be. */
function go(value: string): void {
  const url = new URL(window.location.href);
  url.searchParams.delete("auth");
  if (value === SIGNED_OUT) url.searchParams.set("auth", "out");
  else url.searchParams.set("role", value);
  window.location.assign(url.toString());
}

/**
 * Mount the badge, once, if the design has roles to switch between.
 *
 * Called by mock/browser.ts before the worker starts — and in wired mode, where
 * the worker never starts at all, it is the only thing that module does.
 */
export function mountRoleBadge(): void {
  if (!roles || roles.length === 0) return;
  if (document.getElementById("aep-mock-badge")) return;

  const host = document.createElement("div");
  host.id = "aep-mock-badge";
  host.style.cssText = [
    "position:fixed",
    "right:12px",
    "bottom:12px",
    "z-index:2147483647",
    "font:12px/1.4 ui-monospace,SFMono-Regular,Menlo,monospace",
    "background:rgba(17,17,17,.92)",
    "color:#fff",
    "border-radius:6px",
    "padding:6px 8px",
    "display:flex",
    "gap:8px",
    "align-items:center",
    "box-shadow:0 2px 8px rgba(0,0,0,.35)",
  ].join(";");

  // Says which API is behind the page, because the same screen means different
  // things in the two modes: seed data that always looks right, or a real
  // service that can be wrong.
  const label = document.createElement("span");
  label.textContent = wired ? "wired" : "mock";
  label.style.cssText = "white-space:nowrap;opacity:.75";

  // The two entries beside the roles are the cases a walk must cover and a role
  // name cannot express: signed in holding nothing (NoAccess), and not signed
  // in at all (the app's own sign-in guard).
  const select = document.createElement("select");
  select.id = "aep-mock-badge-role";
  select.setAttribute("aria-label", "Switch role");
  select.style.cssText = "font:inherit;background:#fff;color:#111;border:0;border-radius:4px;padding:2px 4px";
  const options: { value: string; text: string }[] = [
    ...roles.map((role) => ({ value: role, text: role })),
    { value: "", text: "no role" },
    { value: SIGNED_OUT, text: "signed out" },
  ];
  for (const option of options) {
    const element = document.createElement("option");
    element.value = option.value;
    element.textContent = option.text;
    select.appendChild(element);
  }
  const active = activeValue();
  // A role the design does not declare (hand-typed on the URL) shows as "no
  // role" rather than adding itself: the list is the design's, not the URL's.
  select.value = options.some((option) => option.value === active) ? active : "";
  select.addEventListener("change", () => {
    go(select.value);
  });

  host.appendChild(label);
  host.appendChild(select);
  document.body.appendChild(host);
}
