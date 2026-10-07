# Acme Expenses

## Problem Statement

Acme's 40 staff claim expenses on paper forms and email. Receipts get lost, managers approve late, and finance retypes every approved claim into Xero.

## Solution

An expense tracker: staff submit expenses with a receipt photo, managers approve claims, and finance sends approved claims to Xero every night.

## Actors

- Employee: submits expenses and claims.
- Manager: approves or rejects their team's claims.
- Finance: gives second approvals and runs the payroll export.

## Features

- F1 [Submit expenses](features/F1-submit-expenses.md)
- F2 [Approvals](features/F2-approvals.md)
- F3 [Payroll export](features/F3-payroll-export.md)
- F4 [Spending reports](features/F4-spending-reports.md)
- F5 [Mileage claims](features/F5-mileage-claims.md)

## Fog

- Corporate cards, once finance picks a card provider.
- Per-diem rates for overseas trips.
- Read-only access for the yearly external audit.

## Product-wide

Rules that apply to more than one feature, such as the audit log (P1) and company sign-in (P4), are on the [Product-wide](product-wide.md) page.

## Out of Scope

- A mobile app. Staff use the web app on their phones.

## Retired

- F6 Budget alerts dropped
