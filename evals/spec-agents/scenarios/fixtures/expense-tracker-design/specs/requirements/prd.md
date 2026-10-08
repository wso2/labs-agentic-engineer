# Expense Tracker

## Problem Statement

Employees pay for work expenses out of pocket — a taxi to a client, a
conference ticket, a team lunch — and then chase the money through email and
spreadsheets. Nobody can say what has been claimed, what has been decided, and
what Finance still owes, so claims sit unanswered and the monthly total is only
known once somebody rebuilds it by hand.

## Solution

A shared expense tracker where an employee files a claim and follows its
status, an approver works a single queue of everything waiting for a decision
and approves or rejects each one, and Finance reads the monthly total straight
from the same record. One claim, one lifecycle, one place to look.

## Actors

- **Employee** — files expense claims and follows the status of their own
  claims. Sees nothing but their own.
- **Approver** — works the queue of submitted claims, approves or rejects each
  one, and reads the monthly totals. Sees every claim.

## Features

- F1 [Claims](features/F1-claims.md)
- F2 [Approvals](features/F2-approvals.md)
- F3 [Monthly report](features/F3-monthly-report.md)

## Product-wide

Sign-in, who sees which claims, and a claim's states are on the
[Product-wide](product-wide.md) page.

## Out of Scope

- Payment or reimbursement itself — the tracker records the decision, another
  system moves money.
- Receipt image upload and OCR.
- Per-department budgets, approval limits and delegation.
- Email or any other outside notification; everything is in-app.
