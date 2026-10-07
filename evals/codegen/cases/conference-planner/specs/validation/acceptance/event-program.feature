Feature: Event program

  @story-1
  Rule: Only organizers may create and edit events

    Scenario: Olivia creates an event
      Given Olivia is an organizer
      When Olivia creates an event named uniquely for this run in "Colombo" from "2026-11-12" to "2026-11-14"
      Then the event appears in the events list with venue "Colombo"

    @negative
    Scenario: An attendee cannot create an event
      Given Dan is an attendee
      When Dan looks for a way to create an event
      Then the number of events is unchanged

    Scenario: Olivia edits an event's venue
      Given Olivia has created an event with venue "Colombo"
      When Olivia changes the venue to "Kandy"
      Then the event shows venue "Kandy"

  @story-2
  Rule: Organizers define rooms for an event

    Scenario: Adding a room
      Given Olivia has created an event
      When Olivia adds a room "Hall A" with capacity 120
      Then the event lists room "Hall A" with capacity 120

  @story-3
  Rule: A session has a room, a time slot and a capacity

    Scenario: Creating a session
      Given Olivia has created an event with room "Hall A"
      When Olivia creates a session "Intro to APIs" in "Hall A" from "10:00" to "11:00" with capacity 3
      Then the event schedule shows "Intro to APIs" in "Hall A" with 3 seats remaining

  @story-9
  Rule: Any signed-in attendee may browse events and their sessions

    Scenario: Browsing sessions
      Given Olivia has created an event with a session "Intro to APIs"
      When Dan the attendee opens that event
      Then Dan sees the session "Intro to APIs" with its room and time
