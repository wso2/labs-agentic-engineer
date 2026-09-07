# Requirements: Team Lunch Ordering

## Overview

A web app for coordinating team lunch orders. Any teammate can open a daily
lunch round for a chosen restaurant with a cutoff time; teammates add their
own items (with price) before the cutoff; the opener gets one consolidated
order — grouped by item — with a per-person total so they know who owes what
once they place the order and pay the restaurant.

This is a v1 for a single, flat team of ~40 people at one startup. No
multi-team/group structure, no restaurant menu catalog, and no in-app payment
— people settle up with the opener offline.

## Actors

- **Teammate** — any signed-in member of the team; opens rounds, adds and
  edits their own items before cutoff, views the consolidated order and past
  rounds.
- **Opener** — the teammate who opened the current round; may close it early.

## Users & Authentication

- All users belong to a single shared team — there is no team/group hierarchy
  in v1.
- Users sign in with the company's existing Google Workspace account (SSO).
  No separate signup/password flow.
- Any signed-in teammate may open a daily lunch round; there is no special
  "organizer" role. Any signed-in teammate may add items to any open round.

## Core Concepts

- **Round**: a single day's lunch order. Created by an opener, who sets:
  - the restaurant (free-text name, no menu catalog)
  - the cutoff time (date/time after which no more items can be added)
  - optionally, notes (e.g. how to place the order, delivery vs. pickup)
- **Item**: an entry added by a teammate to an open round — their name
  (implicit from login), a free-text description of what they want, a
  quantity, and a price. A teammate may add multiple items to the same round.
- **Consolidated order**: once a round is closed (past cutoff, or manually
  closed by the opener), the system produces a summary for the opener:
  - all items grouped by item description, with quantities
  - a per-person breakdown of what each teammate ordered and owes
  - a grand total for the round

## Functional Requirements

1. **Open a round**: any signed-in user can start a new round for the current
   day by specifying a restaurant name and a cutoff time. Only one round may
   be open at a time (v1 assumes one lunch round per day for the whole team).
2. **Browse open round**: any signed-in user can see the currently open
   round's restaurant, cutoff time, and the items added so far (and by whom).
3. **Add items before cutoff**: any signed-in user can add one or more items
   (description, quantity, price) to the open round, as long as the current
   time is before the round's cutoff. After cutoff, the system rejects new
   items with a clear message.
4. **Edit/remove own items before cutoff**: a user can edit or remove items
   they personally added, as long as the round is still open (before cutoff).
   Users cannot edit or remove another teammate's items.
5. **Automatic close at cutoff**: once the cutoff time passes, the round is
   considered closed — no further items may be added or edited, and it
   becomes read-only history.
6. **Manual close**: the opener may manually close their round before the
   cutoff time (e.g. if it's time to place the order early).
7. **Consolidated view for the opener**: once a round is closed (by cutoff or
   manually), the opener (and any teammate) can view the consolidated order:
   items grouped by description with total quantities, a per-person list of
   items and their individual cost total, and a grand total for the round.
8. **History**: past (closed) rounds remain viewable — who ordered what, from
   where, and on what day — as a simple list/history view.
9. **Slack notifications**: the app posts a message to the team's existing
   Slack channel when a round opens and again at cutoff. Slack is the only
   notification channel — no email.

## Non-Functional / Constraints

- Single flat team, ~40 users; no multi-tenancy or team management needed.
- No payment processing — the app only tracks who owes what; settling up
  happens offline between teammates and the opener.
- No restaurant/menu catalog integration — restaurant and items are
  free-text entered by users.
- Authentication via the company's Google Workspace SSO; no local
  username/password accounts.

## Explicit Assumptions

- Only one lunch round is open at a time for the whole team (no support yet
  for multiple simultaneous rounds, e.g. two different restaurants same day).
- Prices are entered manually by each teammate per item (no live menu
  pricing), so per-person totals are only as accurate as what people type.
- Currency/locale is a single default (e.g. USD) — no multi-currency support.

## Out of Scope (v1)

- Multiple teams/groups, team membership management.
- Restaurant menu catalogs or structured item selection.
- In-app payment, invoicing, or expense-system integration.
- Email notifications and ad-hoc reminder nudges (Slack open/cutoff messages
  ARE in scope — see Functional Requirements #9).
