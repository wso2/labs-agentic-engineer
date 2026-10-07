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

import { renderNestedMarkdownContent } from "@tiptap/core";
import { ListItem, OrderedList } from "@tiptap/extension-list";
import { TableKit } from "@tiptap/extension-table";
import { Markdown } from "@tiptap/markdown";
import StarterKit from "@tiptap/starter-kit";

// The skill editor's schema: what a SKILL.md body can hold, read from and
// written back to markdown.
//
// Tiptap's ordered list treats a start of 0 as "unset" (`|| 1` in its
// tokenizer, its parser and its writer), so a list numbered from 0 came back
// numbered from 1: a real change to the skill's text on any edit. These two
// extensions keep 0, reading it from the list's own first marker.

const parseOrderedList = OrderedList.config.parseMarkdown!;
const renderListItem = ListItem.config.renderMarkdown!;

const OrderedListFromZero = OrderedList.extend({
  parseMarkdown: (token, helpers) => {
    const node = parseOrderedList(token, helpers);
    const fromZero = /^\s*0[.)]\s/.test(token.raw ?? "");
    return fromZero && node && !Array.isArray(node) ? { ...node, attrs: { ...node.attrs, start: 0 } } : node;
  },
});

const ListItemFromZero = ListItem.extend({
  renderMarkdown: (node, h, ctx) => {
    const fromZero = ctx?.parentType === "orderedList" && (ctx.meta?.parentAttrs as { start?: number } | undefined)?.start === 0;
    if (!fromZero) return renderListItem(node, h, ctx);
    return renderNestedMarkdownContent(node, h, (context) => `${context.index ?? 0}. `, ctx, { alignNestedToPrefix: true });
  },
});

export const skillEditorExtensions = [
  StarterKit.configure({ link: { openOnClick: false }, orderedList: false, listItem: false }),
  OrderedListFromZero,
  ListItemFromZero,
  TableKit,
  Markdown,
];
