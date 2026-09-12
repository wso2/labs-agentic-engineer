# Expense Tracker — PRD

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

## User Stories

1. As an Employee, I want to submit an expense claim with a description, an
amount and a date, so that Finance has a record to act on.
2. As an Employee, I want to see my own claims and their current status, so
that I know what has been decided and what is still waiting.
3. As an Approver, I want to see every claim waiting for a decision in one
queue, so that nothing sits unanswered.
4. As an Approver, I want to approve a submitted claim, so that the employee is
reimbursed and the claim leaves the queue.
5. As an Approver, I want to reject a submitted claim with a reason, so that
the employee knows why and can correct it.
6. As an Employee, I want to see the decision and the reason on a claim of
mine, so that I do not have to ask anybody what happened to it.
7. As an Approver, I want to read the monthly total of approved claims, so that
Finance can close the month without rebuilding the figure by hand.

## Product Decisions

- Sign-in is SSO through the platform identity provider. What a person may do
  is decided by the permissions their roles grant, not by anything this app
  stores about them.
- An Employee sees their own claims only; an Approver sees every claim. This is
  a permission difference, not two copies of the data. *assumed*
- Approvers do not file claims in this PRD. Somebody who does both holds both
  roles. *assumed*
- A claim is `submitted`, `approved` or `rejected`. There is no draft state and
  no multi-step approval chain. *assumed*
- A decision is final: an approved or rejected claim cannot be decided again.
  *assumed*
- The monthly report is the total of approved claims per calendar month,
  read-only, no export in this release.

## Out of Scope

- Payment or reimbursement itself — the tracker records the decision, another
  system moves money.
- Receipt image upload and OCR.
- Per-department budgets, approval limits and delegation.
- Email or any other outside notification; everything is in-app.
