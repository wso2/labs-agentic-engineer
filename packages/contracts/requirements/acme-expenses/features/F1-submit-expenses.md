# Submit expenses

## Purpose

Employees record expenses with a receipt photo and submit them as a claim.

## User Stories

- F1.1 As an employee, I photograph a receipt and the amount and date are filled in for me.
- F1.2 As an employee, I pick a category for each expense: meals, travel or supplies. [T&E Policy v3 · p.3]
- F1.3 As an employee, I save expenses as a draft and submit them later as one claim.
- F1.4 As an employee, I see which of my claims are pending, approved or rejected. Needs: F2.

## Decisions

- A receipt is required for any expense above $25. [T&E Policy v3 · p.4]
- Meals are capped at $50 per day. [T&E Policy v3 · p.2]
- A claim can hold expenses from any dates.
- Amounts are entered as P3 describes; a submitted claim goes to F2 for approval.
- A receipt photo is stored under its `receipt_id` and kept **unchanged** after the claim is submitted.
- Expenses paid in a *foreign* currency are converted at the day's rate, as the [rate policy](https://acme.example/rates) sets.
- Paper receipts are ~~accepted~~ scanned on arrival.

## Out of Scope

- Corporate card transactions. Staff pay and claim back.
