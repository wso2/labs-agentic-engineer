# Conference Planner — PRD

## Problem Statement
Conference organizers juggle events, sessions, rooms, speaker proposals, reviews and attendee sign-ups across spreadsheets and inboxes. Proposals get lost, scoring is inconsistent, and session seats are over- or under-booked, with no fair waitlist when a session is full.

## Solution
A web application where organizers build events with sessions and rooms, speakers submit talk proposals, reviewers score them and organizers accept or reject them, and attendees register for events and sessions. Sessions have capacity limits; when full, attendees join a waitlist and the next person is promoted automatically when a seat frees up.

## Actors
- **Organizer** — creates and manages events, rooms and sessions; decides on proposals; sees all registrations.
- **Speaker** — submits and tracks their own talk proposals.
- **Reviewer** — sees proposals assigned or open for review and scores them.
- **Attendee** — browses events and sessions, registers, cancels, and sees their registration and waitlist status.

## User Stories
1. As an organizer, I want to create and edit events, so that I can set up a conference.
2. As an organizer, I want to define rooms for an event, so that sessions have a venue.
3. As an organizer, I want to create sessions with a room, time slot and capacity, so that the schedule is built.
4. As a speaker, I want to submit a talk proposal to an event, so that I can be considered for the program.
5. As a speaker, I want to see the status and outcome of my proposals, so that I know where I stand.
6. As a reviewer, I want to see proposals for an event and score them, so that organizers can decide on merit.
7. As an organizer, I want to see proposal scores and accept or reject each proposal, so that the program is chosen.
8. As an organizer, I want an accepted proposal to be turned into a scheduled session, so that it can take registrations.
9. As an attendee, I want to browse events and their sessions, so that I can decide what to attend.
10. As an attendee, I want to register for an event, so that I am counted as attending.
11. As an attendee, I want to register for a session with limited capacity, so that I secure a seat.
12. As an attendee, I want to join a waitlist when a session is full, so that I may get a seat later.
13. As an attendee, I want to be promoted automatically from the waitlist when a seat frees up, so that I do not have to keep checking.
14. As an attendee, I want to cancel my registration, so that my seat goes to someone else.
15. As an organizer, I want to see registrations, remaining capacity and waitlist per session, so that I can plan rooms.

## Product Decisions
- Sign-in: all users sign in via SSO through the platform identity provider (organization default).
- Roles: organizer, speaker, reviewer and attendee are distinct roles; a person holds one or more. *assumed*
- Registration (capacity, waitlist, promotion) lives in its own registration service, called only by the events API; the web app calls only the events API.
- Waitlist promotion is first-come-first-served and happens immediately when a seat is freed, including on cancellation. *assumed*
- Event registration is open-ended (no capacity); only sessions have capacity. *assumed*
- A reviewer may score each proposal once, on a 1–5 scale; the score shown is the average. *assumed*
- Organizers may assign any reviewer to any proposal; all reviewers see all proposals of an event. *assumed*
- Attendees must be registered for an event before registering for its sessions. *assumed*
- No notifications: users see status changes in the app only.

## Out of Scope
- Email or any other notifications.
- File uploads and object storage (proposals are text only).
- Scheduled or background jobs.
- External integrations (payments, calendars, etc.).
- AI features.

## Open Questions
1. Are there expected maximum sizes (attendees per event, sessions per event) the product should be designed for?

## Further Notes
Interview was not possible in this run; all `*assumed*` items are open to challenge.
