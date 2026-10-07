screen MyTickets "An employee's own tickets with status and SLA due time"
  navbar "IT Help Desk"
  sidebar "My tickets -> MyTickets | New ticket -> NewTicket | Sign out"
  row
    heading "My tickets"
    right
    button "New ticket" primary -> NewTicket
  table "Title | Status | Priority | SLA due" -> MyTicketDetail
    row "VPN not connecting | In progress | High | Today 17:00"
    row "Replace keyboard | Open | Low | Fri 09:00"
    row "Cannot print | Resolved | Medium | Yesterday 11:00"

screen NewTicket "Employee raises a ticket"
  navbar "IT Help Desk"
  sidebar "My tickets -> MyTickets | New ticket -> NewTicket | Sign out"
  heading "New ticket"
  input "Title"
  textarea "Describe the problem"
  select "Priority"
  row
    right
    button "Cancel" -> MyTickets
    button "Submit ticket" primary -> MyTicketDetail

screen MyTicketDetail "An employee follows one ticket, comments and may reopen it"
  navbar "IT Help Desk"
  sidebar "My tickets -> MyTickets | New ticket -> NewTicket | Sign out"
  breadcrumb "My tickets / VPN not connecting"
  heading "VPN not connecting"
  row
    badge "In progress" info
    badge "High" warning
    text "SLA due: today 17:00"
  text "Cannot connect to the VPN from home since this morning."
  divider
  heading "Comments"
  list "Sam (agent): Please restart the client | You: Done, still failing"
  textarea "Write a comment"
  row
    right
    button "Reopen ticket"
    // adds the comment in place, the thread stays on this screen
    button "Add comment" primary

screen Queue "Support agent works the whole queue"
  navbar "IT Help Desk"
  sidebar "Queue -> Queue | Sign out"
  heading "Ticket queue"
  row
    select "Status"
    select "Priority"
    select "Assignee"
    toggle "SLA breached only"
  table "Title | Requester | Status | Priority | Assignee | SLA due" -> TicketWork
    row "VPN not connecting | Ana Diaz | In progress | High | Sam Ortiz | Today 17:00"
    row "Replace keyboard | Lee Chan | Open | Low | Unassigned | Fri 09:00"
    row "Cannot print | Omar Reyes | Reopened | Medium | Priya Nair | Overdue"

screen TicketWork "Support agent triages, assigns, progresses and resolves a ticket"
  navbar "IT Help Desk"
  sidebar "Queue -> Queue | Sign out"
  breadcrumb "Queue / VPN not connecting"
  row
    heading "VPN not connecting"
    right
    button "Assign" primary -> AssignTicket
  row
    badge "Triaged" info
    badge "High" warning
    text "Assignee: Sam Ortiz"
    text "SLA due: today 17:00"
  row
    select "Priority"
    button "Triage"
    button "Start work"
    button "Resolve"
  divider
  heading "Comments"
  list "Ana (requester): Cannot connect | Sam: Please restart the client"
  textarea "Write a comment"
  row
    right
    button "Back to queue" -> Queue
    // adds the comment in place
    button "Add comment" primary

screen AssignTicket "Support agent picks an assignee"
  navbar "IT Help Desk"
  sidebar "Queue -> Queue | Sign out"
  heading "Assign ticket"
  select "Support agent"
  row
    right
    button "Cancel" -> TicketWork
    button "Assign" primary -> TicketWork

screen TeamQueue "Team lead watches the queue and SLA breaches"
  navbar "IT Help Desk"
  sidebar "Team queue -> TeamQueue | Sign out"
  heading "Team queue"
  row
    card "Open | 14 | tickets waiting"
    card "In progress | 9 | being worked"
    card "SLA breached | 3 | need attention"
  row
    select "Status"
    select "Priority"
    select "Assignee"
    toggle "SLA breached only"
  table "Title | Status | Priority | Assignee | SLA due" -> LeadTicket
    row "Cannot print | Reopened | Medium | Priya Nair | Overdue"
    row "VPN not connecting | In progress | High | Sam Ortiz | Today 17:00"

screen LeadTicket "Team lead reviews one ticket and its thread"
  navbar "IT Help Desk"
  sidebar "Team queue -> TeamQueue | Sign out"
  breadcrumb "Team queue / Cannot print"
  row
    heading "Cannot print"
    right
    button "Reassign" primary -> ReassignTicket
  row
    badge "Reopened" warning
    badge "Overdue" danger
    text "Assignee: Priya Nair"
  divider
  heading "Comments"
  list "Omar (requester): Still not printing | Priya: Looking again"
  button "Back to team queue" -> TeamQueue

screen ReassignTicket "Team lead picks a new assignee"
  navbar "IT Help Desk"
  sidebar "Team queue -> TeamQueue | Sign out"
  heading "Reassign ticket"
  select "Support agent"
  row
    right
    button "Cancel" -> LeadTicket
    button "Reassign" primary -> LeadTicket

flow "Raise and follow a ticket"
  role "Employee"
  description "An employee raises a ticket, follows it and may reopen it"
  MyTickets
  NewTicket
  MyTicketDetail

flow "Work the queue"
  role "Support Agent"
  description "An agent triages, assigns, works and resolves tickets"
  Queue
  TicketWork
  AssignTicket

flow "Watch the queue"
  role "Team Lead"
  description "A team lead watches SLA breaches and reassigns tickets"
  TeamQueue
  LeadTicket
  ReassignTicket
