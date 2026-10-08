---
name: acceptance-criteria
description: Use when generating the acceptance criteria — write specs/validation/acceptance/F<n>-<slug>.feature, one Gherkin file per feature, from the requirements alone, for the features a design run designs.
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Generate the acceptance criteria

You are producing the **acceptance criteria** as Gherkin: `Rule:` blocks
that state what the product must do, each illustrated by concrete `Scenario:`
examples. It is written to be read by a person and executed by an agent driving
the live app — there are no step definitions and no generated test code, so the
scenario text IS the test.

**Faithfulness to the requirement matters more than volume.** An invented rule
becomes a check the product was never asked to pass.

## Input — the requirement ONLY

Write criteria for the features this design run designs (`/design F1 F2`
names them; a bare `/design` means every feature it designs). For each, read
its file under `specs/requirements/features/` — its User Stories are the spine
— plus `specs/requirements/prd.md`, whose Actors section names the people your
scenarios are about, and the product-wide items that apply to it.

**Do not read** the design bundle (`design.cell`, `domain-model.md`, `flows/`),
`openapi.yaml`, wireframes, or any source code. The specification must be
independent of the work it will grade.

## Output — Gherkin under `specs/validation/acceptance/`

Write one file per feature: `specs/validation/acceptance/F<n>-<slug>.feature`,
named as the feature's own file is (`features/F1-rounds.md` →
`acceptance/F1-rounds.feature`). Use `addFile`, replacing the contents if the
file already exists; a feature whose design this run updates gets its file
rewritten. Its `Feature:` line is the feature's ID and name.

```gherkin
Feature: F1 Lunch rounds

  @story-F1.1
  Rule: Only one round may be open at a time

    @negative
    Scenario: A second round is refused
      Given a round is open for "Bridge Cafe"
      When Olivia tries to open a round for "Riverside Kitchen"
      Then she is told a round is already open

  @story-F1.1 @story-F1.4
  Rule: Any signed-in teammate may open the day's round

    Scenario: Opening the day's first round
      Given no round is open
      When Olivia opens a round for "Bridge Cafe" at "12:30"
      Then the round is open with a cutoff of "12:30"

    @negative
    Scenario: A cutoff in the past is rejected
      Given no round is open
      When Olivia opens a round for "Bridge Cafe" at "09:00" and the time is "11:00"
      Then she is told the cutoff must be later than now
```

### Structure rules

| Element | Rule |
|---|---|
| `Feature:` | The feature's ID and name: `Feature: F1 Lunch rounds`. One file per feature. |
| `Rule:` | One thing the product must do, in one sentence. This is **your inference** — the PRD has no rules section — so state it plainly enough that a reviewer can reject it. Carry its constraint (see below). |
| `@story-F<n>.<m>` on a `Rule:` | The IDs of the stories the rule comes from — this feature's stories only. Every rule carries at least one. Every story of the feature appears on some rule; the build refuses a retired or unknown ID, and an uncovered story. |
| `Scenario:` | One concrete illustration of its rule. Name the distinguishing circumstance, not the steps. |
| `@negative` | On any scenario whose outcome is the system **refusing, rejecting, or limiting**. See below for where it goes. |
| Steps | `Given` present-tense state · `When` the one action · `Then` the observable outcome. |

### Regenerating over an existing set

Story IDs are permanent by contract, so a tag stays correct across
regeneration. Read what is already in `specs/validation/acceptance/` before writing.

- **Keep the file named after its feature.** A renamed feature renames its
  file (the ID stays); a retired feature's file goes. A file that is not named
  `F<n>-<slug>.feature` is from before features — remove it once every rule it
  held lives in a feature's file.
- **Keep rules that still hold, and their wording.** Rewrite only what the
  requirement actually changed.
- **Each rule lives in exactly one file**, the file of the feature whose
  stories it comes from. A product-wide requirement has no file of its own:
  where it shapes a feature's stories, state it as that feature's rule, tagged
  with that feature's stories.

## Authoring discipline

- **Business language.** Words the PRD uses, for things the product has. Never
  a CSS selector, a route, an HTTP verb, a table name, or a component name.
- **Real data, always.** `"Bridge Cafe"`, `"12:30"`, `"3 items"` — never "a
  valid restaurant", "some time", "appropriate input". A vague step is one an
  agent will satisfy any way it likes, and the check becomes meaningless.
  Concrete data is what makes a scenario falsifiable.
- **Name the actor.** Draw personas from the PRD's Actors section and give them
  names — `Olivia the organizer`, `Dan the diner`. Never `I`: it hides which
  actor is acting and falls apart the moment two of them appear.
- **One scenario, one behaviour.** One `When`. A second `When` after a `Then` is
  a second scenario. Split any "X and Y" into two.
- **A second scenario under a rule must earn its place.** It has to show
  something the first does not — a different outcome, a boundary, a refusal.
  "Olivia extends the cutoff" followed by "Olivia shortens the cutoff" is one
  behaviour written twice; the second buys nothing and costs a run.
- **Brief.** Most scenarios are five steps or fewer. Past that you are usually
  describing a procedure instead of illustrating a rule.
- **Essential only.** Every step must contribute to illustrating *this* rule.
  A password in a scenario about balances is noise.
- **Each scenario stands alone.** Its `Given` steps establish everything it
  needs; it never depends on a scenario above it having run. Note that a
  `Background:` re-runs before every scenario, so it saves writing, not work —
  use it only for state the reader genuinely needs in order to read the file.
- **Own what you assert about.** The app under test is deployed and keeps its
  data, so state a scenario did not create belongs to someone else. Where the
  product has a container — a round, a board, a list — the `Given` creates one
  and every later step stays inside it: `Given Olivia has opened a new round`,
  then assert on *that* round. Owning the container is what makes "stands
  alone" true against a shared database, and it is why nothing has to be reset.
- **Say the state, not the steps that reach it.** `Given a round is open for
  "Bridge Cafe"`, not four steps that open one.
- **Data that gets created needs to survive a second run.** Owning a container
  keeps scenarios apart from each other; it does not keep this run apart from the
  last one. When a scenario creates something whose name must be unique, say so
  in the step — `When Olivia opens a round for a restaurant named uniquely for
  this run` — rather than pinning a literal that collides the next time.

## Cover the refusals, and mark them

The single most common defect in generated acceptance specs is that everything
is a happy path. A rule that only says what works has not been specified: the
interesting half of `Only one round may be open at a time` is the second round
being refused.

For every rule, ask what it forbids, what it limits, who it shuts out — a
capability the requirement puts behind sign-in has a case for someone who is not
signed in — and what it does when the input is wrong. Write those scenarios — but
only where the requirement actually says so. A rule that genuinely refuses
nothing (`the opener sees the consolidated order`) gets no refusal invented for
it; inventing one is a worse defect than omitting it.

**Assert what the refusal DOES, not how the user hears about it.** A `Then`
written as "is told X" presumes the product answers a rejected action with a
message. Plenty of products refuse by not offering the action at all — the
control is absent or disabled — and then no one is ever *told* anything, so the
scenario cannot be satisfied however correct the product is.

That wording also fails the test this skill already applies to steps: if the
sentence would have to change when the implementation changes, it contains
implementation. And a message is not what the rule guarantees. `A bought item
cannot be edited` guarantees the item does not change; whether a toast appears
is a separate product decision, and an unstated one is not yours to invent.

| Instead of | Write |
|---|---|
| `Then he is told "Milk" is already on the list` | `Then the list still has exactly one item` |
| `Then she is told the item name cannot be empty` | `Then no item is added to the list` |
| `Then he is told a bought item cannot be edited` | `Then the quantity of "Milk" is unchanged` |

Both shapes of product satisfy the right-hand column, and both are still
refuted by one that actually allows the action — which is the whole job.

Beware quoting a value inside a "told" step: `is told "milk" is already on the
list` silently demands the product echo the user's own casing back, which
almost none do. State the effect and the quoting problem disappears.

**Count the whole set, not the part that matches what you expected.** An effect
assertion has to be written so the defect it guards against cannot slip past
its own filter. `the list still has exactly one item named "Milk"` reads right
and is vacuous: if the product wrongly accepted `" milk "`, the list now holds
a `"Milk"` AND a `" milk "`, and a count filtered to `"Milk"` is still one — so
the scenario passes on exactly the bug it exists to catch. Count the whole of
the container the scenario owns — `the list still has exactly one item` — which
is only sound because the `Given` created that list.

Where the product has no container to own, count the *change* rather than the
total: `Then one more order is on the round than before`. A wrongly accepted
duplicate makes the change two and the scenario still fails, where a filtered
count would have passed.

The same trap waits wherever a refusal is asserted by looking only at what you
expected to see. Ask what the product would do if the rule were missing, and
make sure the assertion would notice it.

**Where `@negative` goes.** Tags inherit down `Feature` → `Rule` → `Scenario`,
so put it at the level where it is true:

- The rule *permits* something and one scenario shows a refusal → tag **that
  scenario**.
- The rule is *itself* a prohibition (`A teammate cannot change another
  teammate's item`) → tag the **`Rule:`**, and every scenario under it inherits.

Judge each scenario by its own outcome, not by how its rule is worded. A
scenario whose `Then` is "she is told a round is already open" is a refusal and
carries the tag — even when it sits under a rule phrased as a permission. That
one is the easiest to miss.

**State a rule with its constraint, not as a bare permission.** When the
requirement scopes a capability to one actor or one window, that scope IS the
rule, and phrasing it away loses the refusal:

- `Only the opener may close her round early` — the limit is visible, and the
  scenario where someone else tries writes itself.
- `The opener may close her round early` — the same requirement with the limit
  phrased out. Nothing prompts the refusal, and it goes unwritten.

A story that says "As the **opener**, I want to close my round" is scoping the
capability to the opener. Say so in the rule.

## Report ambiguity, don't fill it in

When the requirement is silent or ambiguous, do **not** invent a rule to cover
the gap. Write what the requirement supports, and **list the ambiguities and
any assumptions in your reply to the user** — never in the `.feature` files. An
open question belongs in the conversation; the files stay on-shape.

## Do not

- Do not read the design bundle, `openapi.yaml`, wireframes, or source code.
- Do not write step definitions, test code, or a runner — this skill produces
  `.feature` files and nothing else.
- Do not invent scope, screens, endpoints, or features the requirement lacks.
- Do not put prose, comments, or notes outside the Gherkin structure.
