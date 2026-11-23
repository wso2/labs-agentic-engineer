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

import { Fragment, type ReactNode } from "react";
import { createLink } from "@tanstack/react-router";
import { Box, Button, Checkbox, Link, Typography } from "@wso2/oxygen-ui";
import { useBuildAction } from "../../builds/buildPicker";
import { soft } from "../../spec/components/Tag";
import { dependencyKey } from "../model/artifacts";
import { commentsOn, isWaiting, overviewNote } from "../model/review";
import type { DesignCard, ReviewRow } from "../useDesignCard";
import { AnswerQuestions } from "./QuestionsPanel";

// The Design card's Overview (L22, D12, D13): what to review, for whom, and
// the card's one step forward. Four parts: the business review list
// (prototypes, agents, roles and permissions), the technical one
// (architecture, data model, contracts, flows, technical decisions), the
// Dependencies (what blocks a build first), and the acceptance tests, one row
// per feature, for reference. At the bottom, Address comments (N) and Build,
// the only place comments are sent from. Above them all, while the agent
// waits on the user, its questions (Answer N questions, model/questions.ts).
// Exactly one button is the primary (model/review.ts primaryAction): Answer
// while a question waits, Address comments while a comment waits, Design
// while the spec is ahead of the design, Build once everything is checked,
// otherwise Open on the first unchecked row. The note beside them says what
// the primary asks and, while Build is held, why (model/review.ts overviewNote).

const OpenLink = createLink(Link);
const OpenButton = createLink(Button);

const kicker = {
  fontSize: "0.6875rem",
  letterSpacing: "0.08em",
  textTransform: "uppercase",
  fontWeight: 600,
  color: "text.secondary",
} as const;

function Section({ title, aside, children }: { title: string; aside?: string; children: ReactNode }) {
  return (
    <Box component="section" aria-label={title} sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
      <Box sx={{ display: "flex", alignItems: "baseline", gap: 1, flexWrap: "wrap" }}>
        <Typography component="h3" sx={kicker}>
          {title}
        </Typography>
        <Box sx={{ flex: 1 }} />
        {aside && (
          <Typography variant="caption" color="text.secondary">
            {aside}
          </Typography>
        )}
      </Box>
      <Box
        component="ul"
        sx={{
          m: 0,
          p: 0,
          listStyle: "none",
          border: 1,
          borderColor: "divider",
          borderRadius: 2.5,
          bgcolor: "background.paper",
          "& > li + li": { borderTop: 1, borderColor: "divider" },
        }}
      >
        {children}
      </Box>
    </Box>
  );
}

function Row({ highlight = false, children }: { highlight?: boolean; children: ReactNode }) {
  return (
    <Box component="li" sx={{ px: 1.75, py: 1.25, display: "flex", alignItems: "center", gap: 1.25, flexWrap: "wrap", bgcolor: highlight ? soft("primary") : "transparent" }}>
      {children}
    </Box>
  );
}

function Name({ children, sub, subTone }: { children: ReactNode; sub?: string | undefined; subTone?: "success" | "warning" | undefined }) {
  return (
    <Box sx={{ flex: 1, minWidth: 180 }}>
      <Typography variant="body2">{children}</Typography>
      {sub && (
        <Typography variant="caption" color={subTone ? `${subTone}.main` : "text.secondary"}>
          {sub}
        </Typography>
      )}
    </Box>
  );
}

function Count({ n }: { n: number }) {
  if (n === 0) return null;
  return (
    <Box component="span" aria-label={`${n} waiting ${n === 1 ? "comment" : "comments"}`} sx={{ fontSize: "0.75rem", color: "info.main", whiteSpace: "nowrap" }}>
      💬 {n}
    </Box>
  );
}

/** The technical decisions still assumed: they build as written until someone keeps or changes them. */
function Assumed({ n }: { n: number }) {
  if (n === 0) return null;
  return (
    <Box component="span" aria-label={`${n} assumed ${n === 1 ? "decision" : "decisions"}`} sx={{ fontSize: "0.75rem", color: "warning.main", whiteSpace: "nowrap" }}>
      {n} assumed
    </Box>
  );
}

function Open({ projectName, art, title, primary }: { projectName: string; art: string; title: string; primary: boolean }) {
  return primary ? (
    <OpenButton to="/projects/$projectName/design" params={{ projectName }} search={{ art }} size="small" variant="contained" aria-label={`Open ${title}`}>
      Open
    </OpenButton>
  ) : (
    <OpenLink to="/projects/$projectName/design" params={{ projectName }} search={{ art }} variant="body2" aria-label={`Open ${title}`} sx={{ flexShrink: 0 }}>
      Open
    </OpenLink>
  );
}

function ReviewRows({ projectName, card, rows }: { projectName: string; card: DesignCard; rows: ReviewRow[] }) {
  const { primary, review } = card;
  return rows.map(({ item, checked, changed, waiting, assumed }) => {
    const next = primary?.kind === "open" && primary.key === item.key;
    return (
      <Row key={item.key} highlight={next}>
        <Checkbox
          size="small"
          checked={checked}
          disabled={!review.ready}
          onChange={(e) => review.check(item.path, e.target.checked ? (card.hashes[item.path] ?? null) : null)}
          inputProps={{ "aria-label": `Checked: ${item.title}` }}
          sx={{ p: 0.25 }}
        />
        <Name sub={checked ? "checked" : changed ? "changed since you checked it" : undefined} subTone={checked ? "success" : "warning"}>
          {item.title}
        </Name>
        <Assumed n={assumed} />
        <Count n={waiting} />
        <Open projectName={projectName} art={item.key} title={item.title} primary={next} />
      </Row>
    );
  });
}

export function DesignOverview({
  projectName,
  card,
  featureName,
  ready,
  running,
  onAddress,
}: {
  projectName: string;
  card: DesignCard;
  featureName: (id: string) => string;
  /** Whether a turn can start now. */
  ready: boolean;
  /** A design run is under way. */
  running: boolean;
  onAddress: () => void;
}) {
  const { lists, items, primary, blocking, review } = card;
  const build = useBuildAction(projectName);
  const dependencies = items.filter((i) => i.section === "Dependencies");
  const acceptance = items.filter((i) => i.body.kind === "acceptance");
  const unmatched = blocking.filter((d) => !dependencies.some((i) => i.body.kind === "dependency" && i.body.name.toLowerCase() === d.needs.toLowerCase()));
  const waiting = card.waiting.length;

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2.5 }}>
      {card.questions.length > 0 && (
        <Box
          component="section"
          aria-label="Questions for you"
          sx={{ border: 1, borderColor: "warning.main", bgcolor: soft("warning"), borderRadius: 2.5, px: 1.75, py: 1.25, display: "flex", alignItems: "center", gap: 1.25, flexWrap: "wrap" }}
        >
          <Typography variant="body2" sx={{ flex: "1 1 260px" }}>
            {card.questions.length === 1 ? "The agent has a question for you" : `The agent has ${card.questions.length} questions for you`}: a call only you can make. Your
            answers go back with the next run.
          </Typography>
          <AnswerQuestions card={card} primary={primary?.kind === "answer"} ready={review.ready && !running} />
        </Box>
      )}
      <Typography variant="body2" color="text.secondary">
        Open each file and tick Checked. Comment on anything that is wrong from its 💬, then address your comments below.
      </Typography>
      {lists.business.length > 0 && (
        <Section title="For business review" aside="someone who knows how the business works">
          <ReviewRows projectName={projectName} card={card} rows={lists.business} />
        </Section>
      )}
      {lists.technical.length > 0 && (
        <Section title="For technical review" aside="someone technical">
          <ReviewRows projectName={projectName} card={card} rows={lists.technical} />
        </Section>
      )}
      {(dependencies.length > 0 || blocking.length > 0) && (
        <Section title="Dependencies">
          {dependencies.map((item) => {
            const waits = blocking.filter((d) => item.body.kind === "dependency" && d.needs.toLowerCase() === item.body.name.toLowerCase());
            return (
              <Row key={item.key}>
                <Name
                  sub={waits.length > 0 ? `Needs a decision: ${waits.map((d) => `${featureName(d.featureId)} can't be built until it is settled`).join("; ")}` : "Defined"}
                  subTone={waits.length > 0 ? "warning" : "success"}
                >
                  {item.label}
                </Name>
                <Count n={commentsOn(review.review, item.path).filter(isWaiting).length} />
                <Open projectName={projectName} art={item.key} title={item.title} primary={false} />
              </Row>
            );
          })}
          {unmatched.map((d) => (
            <Row key={d.featureId}>
              <Name sub={`Blocks ${featureName(d.featureId)} only`} subTone="warning">
                {featureName(d.featureId)} waits on {d.needs}
              </Name>
              <Open projectName={projectName} art={dependencyKey(d.featureId)} title={`${d.featureId} waits on ${d.needs}`} primary={false} />
            </Row>
          ))}
        </Section>
      )}
      {acceptance.length > 0 && (
        <Section title="Acceptance tests" aside="for reference: what Build is held to">
          {acceptance.map((item) => (
            <Row key={item.key}>
              <Name>{item.label}</Name>
              <Count n={commentsOn(review.review, item.path).filter(isWaiting).length} />
              <Open projectName={projectName} art={item.key} title={item.title} primary={false} />
            </Row>
          ))}
        </Section>
      )}
      <Box component="section" aria-label="Send and build" sx={{ display: "flex", gap: 1, alignItems: "center", flexWrap: "wrap", pt: 0.5 }}>
        <Button
          size="small"
          variant={primary?.kind === "address" ? "contained" : "outlined"}
          disabled={waiting === 0 || !ready || running || !review.ready}
          onClick={onAddress}
        >
          Address comments ({waiting})
        </Button>
        <Button size="small" variant={primary?.kind === "build" ? "contained" : "outlined"} disabled={!build.label} onClick={build.open}>
          {build.label ?? "Build"}
        </Button>
        <Typography variant="caption" color="text.secondary">
          {overviewNote({ waiting, primary, hold: build.hold }).map((part, i) => (
            <Fragment key={i}>
              {i > 0 && " "}
              {part.kind === "text" ? (
                part.text
              ) : (
                <>
                  {part.version} is building —{" "}
                  <OpenLink to="/projects/$projectName/builds/$version" params={{ projectName, version: part.version }} variant="caption">
                    open it on Builds
                  </OpenLink>
                </>
              )}
            </Fragment>
          ))}
        </Typography>
      </Box>
      {review.problem && (
        <Typography variant="body2" color="error.main">
          {review.problem}
        </Typography>
      )}
    </Box>
  );
}
