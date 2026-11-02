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

import { useState } from "react";
import type { ReviewComment } from "@aep/agent-stream";
import { AgentView } from "@aep/ui-agent-view";
import { Box, Typography } from "@wso2/oxygen-ui";
import { PrototypeEntry } from "../../prototype/components/PrototypeEntry";
import type { DesignItem } from "../model/designFiles";
import { changedSinceChecked, commentsOn, isChecked } from "../model/review";
import type { DesignCard } from "../useDesignCard";
import { ArtifactFrame } from "./ArtifactFrame";
import type { ThreadActions } from "./CommentThreads";
import { DependencyView } from "./DependencyView";
import { AcceptanceList } from "./viewers/AcceptanceList";
import { DecisionsView } from "./viewers/DecisionsView";
import { DocumentView } from "./viewers/DocumentView";
import { ArchitectureArtifact, ContractArtifact } from "./viewers/PackagedViewers";
import { RolesView, SecurityView } from "./viewers/RecordViews";

// One of the design's files, open: its title line (Checked and 💬) over the
// console's own renderer for that kind of file, unchanged (D13): the cell
// diagram, DesignView and OpenApiView, roles then how they are enforced, the
// documents as written, the technical decisions with Keep and Change… on an
// assumed one, an agent in AgentView's tabs, the prototype's entry to its
// full-screen review, and the acceptance tests as a plain list.

function SubHead({ children }: { children: string }) {
  return (
    <Typography component="h3" sx={{ pt: 1, pb: 0.5, fontSize: "0.6875rem", letterSpacing: "0.08em", textTransform: "uppercase", fontWeight: 600, color: "text.secondary" }}>
      {children}
    </Typography>
  );
}

function Body({
  projectName,
  card,
  item,
  primary,
  featureName,
  onReview,
  onCommentOnLine,
}: {
  projectName: string;
  card: DesignCard;
  item: DesignItem;
  primary: boolean;
  featureName: (id: string) => string;
  onReview: (component: string) => void;
  /** Open the file's 💬 on a comment about this line of it. */
  onCommentOnLine: (line: string) => void;
}) {
  const { body } = item;
  switch (body.kind) {
    case "architecture":
      return <ArchitectureArtifact source={body.source} layoutKey={`${projectName}:design`} />;
    case "document":
      return <DocumentView markdown={body.markdown} />;
    case "decisions":
      return <DecisionsView lines={card.decisions} ready={card.review.ready} onKeep={card.keepDecision} onChangeDecision={onCommentOnLine} />;
    case "roles":
      return (
        <>
          <RolesView source={body.roles} />
          <SubHead>How it's enforced</SubHead>
          <SecurityView source={body.security} />
        </>
      );
    case "contract":
      return <ContractArtifact source={body.source} />;
    case "agent":
      return <AgentView spec={body.afm} renderMarkdown={(markdown) => <DocumentView markdown={markdown} />} />;
    case "prototype": {
      const prototype = card.prototypes.get(body.component);
      return prototype ? <PrototypeEntry prototype={prototype} primary={primary} onReview={() => onReview(body.component)} /> : null;
    }
    case "dependency": {
      const waiting = card.blocking.filter((d) => d.needs.toLowerCase() === body.name.toLowerCase());
      return (
        <Box sx={{ display: "flex", flexDirection: "column", gap: 3 }}>
          {waiting.map((d) => (
            <DependencyView key={d.featureId} projectName={projectName} dependency={d} featureName={featureName(d.featureId)} />
          ))}
          <DocumentView markdown={body.markdown} />
        </Box>
      );
    }
    case "acceptance":
      return <AcceptanceList path={body.path} content={body.content} />;
  }
}

/** A prototype comment's place, in words: its screen and elements. */
export function placeIn(card: DesignCard, item: DesignItem): (c: ReviewComment) => string | null {
  const manifest = item.body.kind === "prototype" ? card.prototypes.get(item.body.component)?.files?.manifest : undefined;
  return (c) => {
    if (!c.anchor) return null;
    const screen = manifest?.screens.find((s) => s.id === c.anchor!.screenId)?.name ?? c.anchor.screenId;
    return c.anchor.elementIds.length > 0 ? `${screen} · ${c.anchor.elementIds.join(", ")}` : `${screen} (whole screen)`;
  };
}

export function threadActions(card: DesignCard): ThreadActions {
  const { review } = card;
  return {
    me: review.me.id,
    onEdit: (id, text) => review.editDraft(id, { text }),
    onDelete: (id) => review.deleteDraft(id),
    onReopen: (id) => review.reopen(id),
  };
}

export function ItemPane(props: {
  projectName: string;
  card: DesignCard;
  item: DesignItem;
  /** It is the Overview's next step: its own action is the primary. */
  primary: boolean;
  featureName: (id: string) => string;
  onReview: (component: string) => void;
}) {
  const { card, item } = props;
  const { review } = card;
  const hash = card.hashes[item.path];
  // The line of this file a comment is being written about.
  const [quote, setQuote] = useState<{ path: string; line: string } | null>(null);
  return (
    <ArtifactFrame
      title={item.title}
      path={item.path}
      revision={card.revisions[item.path]}
      check={item.checkable ? { checked: isChecked(review.review, item.path, hash), changed: changedSinceChecked(review.review, item.path, hash) } : null}
      onCheck={(checked) => review.check(item.path, checked && hash !== undefined ? hash : null)}
      comments={commentsOn(review.review, item.path)}
      actions={threadActions(card)}
      onComment={(text) => void review.comment(item.path, text)}
      place={placeIn(card, item)}
      ready={review.ready}
      quote={quote?.path === item.path ? quote.line : null}
      onQuoteDone={() => setQuote(null)}
    >
      <Body {...props} onCommentOnLine={(line) => setQuote({ path: item.path, line })} />
    </ArtifactFrame>
  );
}
