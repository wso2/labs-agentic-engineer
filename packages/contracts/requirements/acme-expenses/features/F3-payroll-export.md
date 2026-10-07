# Payroll export

## Purpose

Finance sends approved claims to Xero every night and fixes the ones that fail.

Needs: F2.

## User Stories

- F3.1 As finance, I have approved claims sent to Xero every night. [T&E Policy v3 · p.9]
- F3.2 As finance, I see which claims failed to sync and why.
- F3.3 As finance, I resend a failed claim after fixing it.

## Decisions

- Xero accepts up to 100 invoices per API call. [Xero API docs]
- Each claim becomes one bill in Xero, with one line per expense.
- A claim over $1,000 is sent only after its second approval (F2.5).

## Open Questions

1. Does finance post to one Xero organisation, or one per country? *blocking*
   - Finance posts every claim to one Xero organisation.
   - Each country has its own Xero organisation; a claim goes to the employee's country.
2. Should a failed sync email finance, or only show on the failures list?
