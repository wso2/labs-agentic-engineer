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

// How a markdown document reads in a Tiptap editor: the spec's files, and a
// skill's body. One look, so the two editors read alike; each adds what is
// its own (the spec's line decorations) on top.
export const proseSx = {
  "& .ProseMirror": { outline: "none", maxWidth: "72ch", fontSize: "0.875rem", lineHeight: 1.55 },
  "& .ProseMirror h1": { fontSize: "1.5rem", fontWeight: 600, letterSpacing: "-0.01em", mt: 0.25, mb: 1.25 },
  "& .ProseMirror h2": { fontSize: "0.9375rem", fontWeight: 600, mt: 2.75, mb: 0.75 },
  "& .ProseMirror p": { my: 0.5 },
  "& .ProseMirror ul, & .ProseMirror ol": { pl: 2.5, my: 0.5 },
  "& .ProseMirror li > p": { my: 0 },
  "& .ProseMirror li + li": { mt: 0.25 },
  "& .ProseMirror a": { color: "primary.main", textUnderlineOffset: "2px", cursor: "pointer" },
} as const;
