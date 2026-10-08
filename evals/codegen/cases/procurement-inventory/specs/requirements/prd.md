# Procurement and Stock — PRD

## Problem Statement
Staff who need goods have no single place to request them, get spending approved within each approver's authority, and record what arrives. Stock levels are tracked ad hoc, so nobody can trust what is on hand or see what is running low. Today's workaround (messages and spreadsheets) causes unapproved spend, lost receipts and stock-outs.

## Solution
A procurement and stock system. Requesters raise purchase requests for catalogue items; approvers approve or reject them within the approval limit; warehouse clerks maintain the item catalogue and receive goods against approved orders, which raises stock. Every stock change is recorded as a ledger entry, and a low-stock view lists items below their reorder level.

## Actors
- Requester: browses the catalogue, raises purchase requests and follows their status.
- Approver: reviews pending requests and approves or rejects those within the approval limit.
- Warehouse Clerk: adds and edits catalogue items, receives goods against approved orders, and views stock levels, the ledger and the low-stock view.

## User Stories
1. As a Requester, I want to browse the catalogue of items, so that I can choose what to request.
2. As a Requester, I want to raise a purchase request for catalogue items and quantities, so that I can obtain goods I need.
3. As a Requester, I want to see the status of my purchase requests, so that I know whether they were approved, rejected or received.
4. As an Approver, I want to see pending purchase requests, so that I can review them.
5. As an Approver, I want to approve or reject a request within the approval limit, so that spending stays under control.
6. As an Approver, I want requests above the approval limit to be refused for approval, so that the limit is enforced.
7. As a Warehouse Clerk, I want to receive goods against an approved order, so that stock is raised to match what arrived.
8. As a Warehouse Clerk, I want every stock change recorded as a ledger entry, so that stock movements are traceable.
9. As a Warehouse Clerk, I want to view the ledger for an item, so that I can see how its level came about.
10. As a Warehouse Clerk, I want a low-stock view listing items below their reorder level, so that I know what needs replenishing.
11. As a Warehouse Clerk, I want to add a catalogue item with its name, unit, unit price and reorder level, so that it can be requested and stocked.
12. As a Warehouse Clerk, I want to edit an item's name, unit, unit price and reorder level, so that the catalogue stays correct.

## Product Decisions
- Sign-in: all users sign in via SSO through the platform identity provider (org default).
- Stock (items, levels, ledger) is held in its own inventory service that only the procurement API calls; the web app calls only the procurement API.
- Stock changes only through receiving goods; each change writes a ledger entry. *assumed*
- Approval limit: one configured value of 5000 (maximum total request value) for all approvers; there are no per-approver limits.
- Receiving may be partial, and an order remains open until fully received. *assumed*
- A requester cannot approve their own request. *assumed*
- Warehouse clerks manage the item catalogue in the app; a new item starts with 0 in stock.
- Each item has a reorder level; low stock means on-hand quantity below it. *assumed*

## Out of Scope
- Email or any notifications.
- File uploads or object storage.
- Scheduled or background jobs.
- External integrations.
- AI features or agents.
- Stock issues or adjustments other than receiving; deleting catalogue items.

## Open Questions
None.
