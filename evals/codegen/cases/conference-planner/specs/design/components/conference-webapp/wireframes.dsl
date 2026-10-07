screen Events "Attendee home: browse events"
  navbar "Conference Planner | Sign out"
  sidebar "Events -> Events | My registrations -> MyRegistrations"
  heading "Events"
  table "Event | Venue | Dates" -> EventDetail
    row "DevSummit 2026 | Colombo | 12-14 Nov"
    row "CloudDays | Kandy | 3-4 Dec"

screen EventDetail "An event with its sessions and registration"
  navbar "Conference Planner | Sign out"
  sidebar "Events -> Events | My registrations -> MyRegistrations"
  breadcrumb "Events / DevSummit 2026"
  heading "DevSummit 2026"
  row
    badge "Not registered" info
    right
    button "Register for event" primary // completes in place
  table "Session | Room | Time | Seats left | Waitlist"
    row "Intro to APIs | Hall A | 12 Nov 10:00 | 3 | 0"
    row "Scaling Queues | Hall B | 12 Nov 11:00 | 0 | 4"
  row
    button "Register for session"
    button "Join waitlist"

screen MyRegistrations "Attendee's own registrations and waitlist status"
  navbar "Conference Planner | Sign out"
  sidebar "Events -> Events | My registrations -> MyRegistrations"
  heading "My registrations"
  table "Item | Status | Waitlist position"
    row "DevSummit 2026 | Confirmed | -"
    row "Scaling Queues | Waitlisted | 2"
  button "Cancel selected" danger

screen MyProposals "Speaker home: status of own proposals"
  navbar "Conference Planner | Sign out"
  sidebar "My proposals -> MyProposals | Submit proposal -> SubmitProposal"
  row
    heading "My proposals"
    right
    button "New proposal" primary -> SubmitProposal
  table "Title | Event | Status"
    row "Scaling Queues | DevSummit 2026 | Accepted"
    row "Intro to APIs | CloudDays | Submitted"

screen SubmitProposal "Speaker submits a talk proposal"
  navbar "Conference Planner | Sign out"
  sidebar "My proposals -> MyProposals | Submit proposal -> SubmitProposal"
  heading "Submit a proposal"
  select "Event"
  input "Talk title"
  textarea "Abstract"
  row
    right
    button "Cancel" -> MyProposals
    button "Submit" primary -> MyProposals

screen ReviewQueue "Reviewer home: proposals to score"
  navbar "Conference Planner | Sign out"
  sidebar "Review queue -> ReviewQueue"
  heading "Review queue"
  select "Event"
  table "Title | Speaker | Average score | Reviews" -> ReviewProposal
    row "Scaling Queues | A. Perera | 4.2 | 5"
    row "Intro to APIs | K. Silva | - | 0"

screen ReviewProposal "Reviewer scores one proposal"
  navbar "Conference Planner | Sign out"
  sidebar "Review queue -> ReviewQueue"
  breadcrumb "Review queue / Scaling Queues"
  heading "Scaling Queues"
  text "Abstract of the talk"
  select "Score 1 to 5"
  row
    right
    button "Back" -> ReviewQueue
    button "Submit score" primary -> ReviewQueue

screen ManageEvents "Organizer home: events"
  navbar "Conference Planner | Sign out"
  sidebar "Manage events -> ManageEvents | Proposals -> OrganizerProposals"
  row
    heading "Events"
    right
    button "New event" primary -> EventForm
  table "Event | Venue | Dates" -> EventAdmin
    row "DevSummit 2026 | Colombo | 12-14 Nov"

screen EventForm "Organizer creates or edits an event"
  navbar "Conference Planner | Sign out"
  sidebar "Manage events -> ManageEvents | Proposals -> OrganizerProposals"
  heading "Event"
  input "Name"
  input "Venue"
  row
    input "Start date"
    input "End date"
  row
    right
    button "Cancel" -> ManageEvents
    button "Save" primary -> ManageEvents

screen EventAdmin "Organizer manages rooms and sessions of an event"
  navbar "Conference Planner | Sign out"
  sidebar "Manage events -> ManageEvents | Proposals -> OrganizerProposals"
  breadcrumb "Events / DevSummit 2026"
  heading "DevSummit 2026"
  row
    heading "Rooms"
    right
    button "Add room" -> RoomForm
  table "Room | Capacity"
    row "Hall A | 120"
  row
    heading "Sessions"
    right
    button "Add session" primary -> SessionForm
  table "Session | Room | Time | Capacity | Waitlist" -> SessionRegistrations
    row "Intro to APIs | Hall A | 12 Nov 10:00 | 100 | 0"

screen RoomForm "Organizer adds a room"
  navbar "Conference Planner | Sign out"
  sidebar "Manage events -> ManageEvents | Proposals -> OrganizerProposals"
  heading "Add room"
  input "Room name"
  input "Capacity"
  row
    right
    button "Cancel" -> EventAdmin
    button "Save" primary -> EventAdmin

screen SessionForm "Organizer creates a session, or schedules an accepted proposal"
  navbar "Conference Planner | Sign out"
  sidebar "Manage events -> ManageEvents | Proposals -> OrganizerProposals"
  heading "Session"
  input "Title"
  select "Room"
  row
    input "Start time"
    input "End time"
  input "Capacity"
  row
    right
    button "Cancel" -> EventAdmin
    button "Save" primary -> EventAdmin

screen SessionRegistrations "Organizer sees seats and waitlist of a session"
  navbar "Conference Planner | Sign out"
  sidebar "Manage events -> ManageEvents | Proposals -> OrganizerProposals"
  breadcrumb "DevSummit 2026 / Intro to APIs"
  heading "Intro to APIs"
  row
    card "Seats left | 3 | of 100"
    card "Waitlist | 0 | waiting"
  table "Attendee | Status | Waitlist position"
    row "A. Perera | Confirmed | -"
    row "K. Silva | Waitlisted | 1"

screen OrganizerProposals "Organizer reviews scored proposals"
  navbar "Conference Planner | Sign out"
  sidebar "Manage events -> ManageEvents | Proposals -> OrganizerProposals"
  heading "Proposals"
  select "Event"
  table "Title | Speaker | Average score | Status" -> ProposalDecision
    row "Scaling Queues | A. Perera | 4.2 | Submitted"

screen ProposalDecision "Organizer accepts or rejects one proposal"
  navbar "Conference Planner | Sign out"
  sidebar "Manage events -> ManageEvents | Proposals -> OrganizerProposals"
  breadcrumb "Proposals / Scaling Queues"
  heading "Scaling Queues"
  text "Abstract of the talk"
  card "Average score | 4.2 | from 5 reviewers"
  row
    right
    button "Reject" danger -> OrganizerProposals
    button "Accept and schedule" primary -> SessionForm

flow "Browse and register"
  role "Attendee"
  description "An attendee browses events, registers and checks their status"
  Events
  EventDetail
  MyRegistrations

flow "Submit a talk"
  role "Speaker"
  description "A speaker submits a proposal and follows its outcome"
  MyProposals
  SubmitProposal

flow "Score proposals"
  role "Reviewer"
  description "A reviewer scores proposals"
  ReviewQueue
  ReviewProposal

flow "Build the program"
  role "Organizer"
  description "An organizer sets up events, rooms and sessions and watches registrations"
  ManageEvents
  EventForm
  EventAdmin
  RoomForm
  SessionForm
  SessionRegistrations

flow "Decide proposals"
  role "Organizer"
  description "An organizer accepts or rejects proposals and schedules accepted ones"
  OrganizerProposals
  ProposalDecision
  SessionForm
