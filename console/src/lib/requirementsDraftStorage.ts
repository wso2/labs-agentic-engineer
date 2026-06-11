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

// Per-file local-storage draft persistence for the Requirements page.
// Drafts survive pod restarts and accidental nav-aways; on reload the
// page compares each draft to its server content and offers a
// "use local / discard" prompt when they diverge.

export interface StoredFileDraft {
  draft: string;
  baseServerContent: string;
  savedAt: number;
}

export type StoredDraftMap = Record<string, StoredFileDraft>;

const PREFIX = 'asdlc:requirements-drafts';

function key(orgId: string, projectId: string): string {
  return `${PREFIX}:${orgId}:${projectId}`;
}

function storage(): Storage | null {
  try {
    return typeof window !== 'undefined' ? window.localStorage : null;
  } catch {
    return null;
  }
}

export function loadDrafts(orgId: string, projectId: string): StoredDraftMap {
  const s = storage();
  if (!s) return {};
  const raw = s.getItem(key(orgId, projectId));
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw) as StoredDraftMap;
    if (typeof parsed !== 'object' || parsed === null) {
      s.removeItem(key(orgId, projectId));
      return {};
    }
    return parsed;
  } catch {
    s.removeItem(key(orgId, projectId));
    return {};
  }
}

export function saveDraft(
  orgId: string,
  projectId: string,
  filename: string,
  draft: StoredFileDraft,
): void {
  const s = storage();
  if (!s) return;
  const all = loadDrafts(orgId, projectId);
  all[filename] = draft;
  try {
    s.setItem(key(orgId, projectId), JSON.stringify(all));
  } catch (err) {
    console.warn('[requirementsDraftStorage] save failed:', err);
  }
}

export function clearDraft(orgId: string, projectId: string, filename: string): void {
  const s = storage();
  if (!s) return;
  const all = loadDrafts(orgId, projectId);
  if (!(filename in all)) return;
  delete all[filename];
  if (Object.keys(all).length === 0) {
    s.removeItem(key(orgId, projectId));
    return;
  }
  try {
    s.setItem(key(orgId, projectId), JSON.stringify(all));
  } catch (err) {
    console.warn('[requirementsDraftStorage] save failed:', err);
  }
}

export function clearAllDrafts(orgId: string, projectId: string): void {
  const s = storage();
  if (!s) return;
  s.removeItem(key(orgId, projectId));
}
