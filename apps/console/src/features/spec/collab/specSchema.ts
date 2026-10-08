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

import { getSchema } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { AgentInsertion } from "@aep/collab-doc";

// The document schema every peer of the spec room shares. It MUST be the
// extension set @aep/collab-doc parses and serializes markdown with
// (StarterKit + AgentInsertion), or content the editor cannot represent is
// dropped on the next write. The editor adds behaviour on top (collaboration,
// decorations) but no node or mark of its own.
export const specSchema = getSchema([StarterKit, AgentInsertion]);
