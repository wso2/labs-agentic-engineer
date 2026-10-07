# ADR-0002 — The agent's questions are answered on a Questions card; the chat points to it

**Status:** Accepted · 2026-10-06
**Supersedes:** point 4 ("a single `QuestionCard` renders one question
compactly or several as a form") of console
[ADR-0012](https://github.com/wso2/labs-agentic-engineer/blob/classic-console/apps/console/design/decisions/ADR-0012-agent-hitl-tool-call-question-cards.md)
(at the `classic-console` tag); the rest of ADR-0012 stands. Spec: #879.

## Context

The agent asks through `ask_question` (one) and `ask_questions` (a batch of up
to 8, each with up to 5 described options and a free answer). The console
rendered either as a card inline in the chat. A spec interview's batch is a
wall of options in the narrow chat column: the user loses track of what is
answered and finds Send only after scrolling past all of it.

## Decision

1. **Every question, one or a batch, is answered on the Questions card**
   (`/projects/$p/questions`, a titled card over the overview). It lists every
   open question at once, numbered, with its options and a free answer, and
   sends them as ONE message (`Answers:` for a batch, `Answer to "…"` for one),
   so the wire, the agent and the serializers in `@aep/agent-stream` do not
   change.
2. **The chat points to it, and the card opens itself only where it is
   wanted.** While questions are open the chat shows a pointer ("The agent
   has N questions · Answer them →"); once answered or superseded, their
   texts, as before. The card opens by itself as the first question of a
   batch lands when the asking turn is **this browser's own** (sent from here,
   or the kickoff of a project created here) and the user is on the project's
   **overview or one of its cards** (`opensQuestionsCard`); once per batch,
   so a card the user closed stays closed. Elsewhere the pointer's click opens
   it. **A send that went through closes the card** back to the overview.
3. **The answer goes back in the scope the question was asked in**: that of
   the message that started the asking turn (`askedScope`), not the scope of
   the page the user answers from, so a feature's interview carries on in that
   feature.
4. **Send waits for every answer and for the whole batch.** Questions can be
   answered as they arrive; pressing Send with gaps flags them instead of
   sending. **Nothing is pre-selected**: the agent's `recommended` option is a
   label, never a default.

## Rejected

- **A card inline in the chat**, stacked (ADR-0012's "several as a form") or
  one question per page with tabs (built and tried in #882): the chat column
  is the wrong place for a form of this size, paginated or not.
- **The Spec card's body as the place** (the classic console's
  `SpecQuestionForm`): the agent also asks from design and prototype turns, and
  a design question must not hide the spec.
- **Opening the card for every question, from any page:** the classic
  console did, and dropped it because it pulled users off the page they were
  on. Here it opens only over the overview, and never for a teammate's turn
  or for questions read back from the history (a reload would reopen a card
  the user closed).
- **"Use recommended answers"** and pre-selecting the recommended option: the
  agent's guesses read back as the user's decisions.
- **Sending a partial batch:** it changes what the agent receives; a free
  answer ("no preference") already covers a question the user would skip.

## Consequences

- The draft is each user's own, kept per question while the page lives
  (closing and opening the card keeps it; a reload does not). A room-shared
  draft, as the classic console had, is a feature of its own.
- A message typed in the chat still supersedes the open questions (#433),
  which empties the card.
- User messages in the chat log now keep their turn's scope, read from the
  history's `scope`; the feature scope carries only the feature's ID.
