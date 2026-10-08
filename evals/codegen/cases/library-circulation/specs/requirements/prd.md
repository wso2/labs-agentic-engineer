# Library Circulation — PRD

## Problem Statement
Library members cannot easily find titles, borrow copies, reserve items that are out, or see what they owe. Librarians manage titles, copies, checkouts, returns and holds by hand, and late fees are calculated inconsistently.

## Solution
A library circulation system. Members search the catalogue, borrow copies, place holds and see their loans and fees. Librarians manage titles and copies, check items out and in, and work the holds queue. Every loan has a due date, and a late return computes a fee. The catalogue (titles, copies) lives in its own catalogue service that only the circulation API calls; the web app calls only the circulation API.

## Actors
- Member: searches the catalogue, borrows copies, places and cancels own holds, and sees own loans and fees.
- Librarian: manages titles and copies, checks items out and in, and works the holds queue.

## User Stories
1. As a member, I want to search the catalogue by title, author or subject, so that I can find what I want to borrow.
2. As a member, I want to see whether a title has available copies, so that I know whether I can borrow it or must place a hold.
3. As a member, I want to borrow an available copy, so that I can take it home.
4. As a member, I want to place a hold on a title with no available copy, so that I get it when it is returned.
5. As a member, I want to cancel my own hold, so that I am no longer in the queue.
6. As a member, I want to see my current loans with their due dates, so that I know when to return them.
7. As a member, I want to see my fees, so that I know what I owe.
8. As a librarian, I want to add, edit and remove titles, so that the catalogue is accurate.
9. As a librarian, I want to add, edit and withdraw copies of a title, so that holdings are accurate.
10. As a librarian, I want to check a copy out to a member, so that the loan is recorded with a due date.
11. As a librarian, I want to check a copy in, so that the loan closes and the copy becomes available or goes to the next hold.
12. As a librarian, I want a fee computed automatically on a late return, so that fees are consistent.
13. As a librarian, I want to view and work the holds queue per title, so that returned copies go to the first member waiting.

## Product Decisions
- Sign-in: all users sign in via SSO through the platform identity provider; roles are member and librarian.
- Architecture: the catalogue service is called only by the circulation API; the web app calls only the circulation API.
- Loan period: 14 days from checkout. *assumed*
- Late fee: a flat rate per day overdue, 0.25 per day, capped at the replacement value is not applied; no cap. *assumed*
- Loan limits: a member may hold at most 5 active loans. *assumed*
- Holds queue: first come, first served; a member may have one hold per title. *assumed*
- Fee payment: fees are displayed only; settling them happens at the desk and a librarian marks them paid. *assumed*
- Members cannot borrow while they have unpaid fees. *assumed*
- Fees are computed when the item is returned, not by background jobs.
- No email or notifications, file uploads or object storage, scheduled or background jobs, external integrations, or AI features.

## Out of Scope
- Email or any other notifications.
- File uploads, cover images, object storage.
- Scheduled or background jobs (e.g. automatic overdue processing).
- External integrations and online payment.
- AI features.
- Loan renewals.

## Open Questions
1. What currency and exact fee rate does the library use? (0.25 per day is assumed.)
