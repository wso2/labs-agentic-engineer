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

// Fixture: rows with more actions than fit inline (a theme may put them behind
// one overflow menu) beside a row with one, in a section with a count.

import { Heading, Screen, Section, Table, defineApp, useCollection, useParams } from "@wso2/prototype-kit";

interface Doc {
  id: string;
  title: string;
  pages: number;
  state: "draft" | "published";
}

const documents: Doc[] = [
  { id: "doc-1", title: "Leave policy", pages: 4, state: "published" },
  { id: "doc-2", title: "Travel guide", pages: 12, state: "draft" },
];

function Documents() {
  const docs = useCollection<Doc>("documents");
  return (
    <Screen>
      <Heading id="heading.documents" text="Documents" />
      <Section id="section.documents" title="All documents" count={docs.items.length}>
        <Table
          id="table.documents"
          columns={["Title", { label: "Pages", kind: "number" }, { label: "State", kind: "status" }]}
          rows={docs.items.map((d) => ({
            id: `row.${d.id}`,
            cells: [d.title, String(d.pages)],
            status: d.state === "published" ? { text: "Published", tone: "success" as const } : { text: "Draft", tone: "default" as const },
            to: "screen.document",
            params: { documentId: d.id },
            actions:
              d.state === "draft"
                ? [
                    { id: `row.${d.id}.open`, label: "Open", to: "screen.document", params: { documentId: d.id } },
                    { id: `row.${d.id}.publish`, label: "Publish", onPress: () => docs.update(d.id, { state: "published" }) },
                    { id: `row.${d.id}.delete`, label: "Delete", emphasis: "danger" as const, onPress: () => docs.remove(d.id) },
                  ]
                : [{ id: `row.${d.id}.open`, label: "Open", to: "screen.document", params: { documentId: d.id } }],
          }))}
        />
      </Section>
    </Screen>
  );
}

function Document() {
  const { documentId } = useParams();
  const docs = useCollection<Doc>("documents");
  const doc = (documentId ? docs.get(documentId) : undefined) ?? docs.items[0];
  return (
    <Screen>
      <Heading id="heading.document" text={doc?.title ?? "Document"} />
    </Screen>
  );
}

export default defineApp({ screens: { "screen.documents": Documents, "screen.document": Document }, data: { documents } });
