# Team Lunch Ordering

## Problem Statement

A team of about 40 people at one startup orders lunch together. Someone picks a
restaurant, everyone replies with what they want, and whoever places the order
has to tally the replies by hand and work out who owes what once they have paid
the restaurant.

## Solution

A web app for coordinating team lunch orders. Any teammate can open a daily
lunch round for a chosen restaurant with a cutoff time; teammates add their own
items (with price) before the cutoff; the opener gets one consolidated order —
grouped by item — with a per-person total, so they know who owes what once they
place the order and pay the restaurant.

## Actors

- **Teammate** — any signed-in member of the one shared team. Opens a daily
  round (becoming its **opener**), adds items to any open round, and reads the
  consolidated order and past rounds. There is no special organizer role.

## Features

- F1 [Rounds](features/F1-rounds.md)
- F2 [Ordering](features/F2-ordering.md)
- F3 [Consolidated order](features/F3-consolidated-order.md)
- F4 [Slack notifications](features/F4-slack-notifications.md)

## Product-wide

Sign-in, the single team and currency are on the [Product-wide](product-wide.md)
page.

## Out of Scope

- Multiple teams or groups, and team membership management.
- Restaurant menu catalogs or structured item selection — restaurant and items
  are free text.
- In-app payment, invoicing or expense-system integration; settling up happens
  offline between teammates and the opener.
- Email notifications and ad-hoc reminder nudges.
