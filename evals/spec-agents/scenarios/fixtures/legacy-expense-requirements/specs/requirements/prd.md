# Expense management — PRD

## Problem Statement
Employees submit expenses through ad-hoc channels; managers approve by email;
finance cannot see outstanding liability until month-end.

## Solution
A web application where employees submit expenses, managers approve them, and
finance pays approved claims — with clear status at every step.

## Actors
- Employee: submits and tracks their own expenses
- Manager: approves or rejects team expenses
- Finance admin: pays approved expenses and audits history

## User Stories
1. As a Employee, I want to submit an expense with receipts, so that I can be reimbursed.
2. As a Employee, I want to see the status of my expenses, so that I know what is pending.
3. As a Manager, I want to approve or reject team expenses, so that policy is enforced.
4. As a Finance admin, I want to mark approved expenses as paid, so that books stay current.
5. As a Employee, I want to sign in with company SSO, so that I do not manage another password.

## Product Decisions
- Sign-in via the company identity provider (SSO)
- Receipt upload is required above a configurable amount *assumed*
- Notifications are in-app only for v1

## Out of Scope
- Payroll / bank transfer execution
- Multi-currency FX rate feeds (use submitted currency as-is)
- Native mobile apps

## Open Questions
1. deferred — does not block design: exact receipt retention period
