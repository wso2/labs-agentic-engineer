# Rounds

## Purpose

A round is one day's lunch order: a teammate opens it for a restaurant with a
cutoff, and it closes at the cutoff or when the opener closes it.

## User Stories

- F1.1 As a teammate, I open a round for today by naming a restaurant and a cutoff time, with optional notes on how to place the order.
- F1.2 As a teammate, I see the open round's restaurant, cutoff and the items added so far, and by whom.
- F1.3 As a teammate, I see a round close by itself once its cutoff passes, after which nobody can add or change items.
- F1.4 As the opener, I close my round before its cutoff when it is time to place the order early.
- F1.5 As a teammate, I look back at past rounds: who ordered what, from where, on which day.

## Decisions

- Only one round is open at a time for the whole team: one lunch round per day. *assumed*
- The restaurant is a free-text name; there is no menu catalog.
