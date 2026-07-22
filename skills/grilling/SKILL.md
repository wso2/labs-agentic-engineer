---
name: grilling
description: Use when asked to grill or interview the user about an idea, spec, or plan before generating — relentless one-question-at-a-time clarification via the ask_question tool until shared understanding.
metadata:
  aep:
    kind: platform
---

# Grilling

Interview the user relentlessly about the idea until you share an
understanding, then generate. The goal is to surface every decision the spec
depends on — purpose, users, scope, constraints, success criteria — before a
single file is written. **Do not mutate files while the interview is open.**

## The interview

- Ask with the **`ask_question` tool, one call per turn** — the turn ends at
  the call, so one question at a time is structural. Never batch questions
  into one call or one turn; never ask in prose what the tool can carry.
- Give **2–5 options**, each with a one-line `description` naming its
  trade-off or implication, and mark **exactly one** option `recommended` —
  your considered answer, not a coin flip.
- Walk each branch of the design tree, resolving dependencies between
  decisions in order: ask the load-bearing questions first (who is it for,
  what must it do), and let later questions build on earlier answers.

## Don't ask what the files answer

If the CURRENT STATE spec bundle already answers a question, read it instead
of asking. Only the user can decide preferences, priorities, and trade-offs;
everything else is your homework.

## Answers

The answer arrives as the next user message, normally serialized as
`Answer to "<question>": <label>[, <label>] — <note>`. A free-typed reply is
an equally valid answer — take it as the response to the open question and
never re-ask a question the user has already answered in prose.

## The skip valve

If the user says anything like "skip", "just generate", or "enough
questions", stop interviewing immediately and proceed on stated assumptions —
name the assumptions you are making in your reply.

## Stopping

Interview until the requirements are unambiguous — there is no fixed question
count; a simple idea may need two questions, a vague one many. When shared
understanding is reached (or the skip valve fires), say what you understood
in a sentence or two, then proceed with the generation instruction.
