Feature: Registration and waitlist

  @story-10
  Rule: Attendees register for an event

    Scenario: Registering for an event
      Given an event named uniquely for this run exists
      When Dan the attendee registers for the event
      Then Dan's registrations show the event as confirmed

    @negative
    Scenario: Registering twice does not duplicate
      Given Dan is registered for an event
      When Dan registers for the same event again
      Then Dan has exactly one registration for that event

  @story-11
  Rule: A session seat is confirmed only while capacity remains, and only for attendees registered for the event

    Scenario: Taking the last seat
      Given a session with capacity 1 and Dan registered for its event
      When Dan registers for the session
      Then Dan's registration for the session is confirmed
      And the session shows 0 seats remaining

    @negative
    Scenario: Not registered for the event
      Given a session with capacity 3 and Erin not registered for its event
      When Erin registers for the session
      Then the session still shows 3 seats remaining

  @story-12
  Rule: A full session puts the next attendee on the waitlist in order

    Scenario: Joining the waitlist
      Given a session with capacity 1 whose only seat is held by Dan
      When Erin registers for the session
      Then Erin's registration is waitlisted at position 1

    Scenario: Waitlist order follows arrival
      Given a session with capacity 1 held by Dan and Erin waitlisted
      When Frank registers for the session
      Then Frank's registration is waitlisted at position 2

  @story-13
  Rule: When a seat frees, the first waitlisted attendee is promoted automatically

    Scenario: Promotion after cancellation
      Given a session with capacity 1 held by Dan with Erin at position 1 and Frank at position 2
      When Dan cancels his registration
      Then Erin's registration is confirmed
      And Frank is waitlisted at position 1

  @story-14
  Rule: Attendees cancel only their own registrations

    Scenario: Cancelling a registration
      Given Dan holds a seat in a session with capacity 3
      When Dan cancels his registration
      Then the session shows 3 seats remaining

    @negative
    Scenario: Another attendee's registration cannot be cancelled
      Given Dan holds a seat in a session with capacity 3
      When Erin tries to cancel Dan's registration
      Then the session still shows 2 seats remaining

  @story-15
  Rule: Only organizers see all registrations, remaining capacity and waitlist of a session

    Scenario: Organizer views a session
      Given a session with capacity 1 held by Dan with Erin waitlisted
      When Olivia the organizer opens the session's registrations
      Then Olivia sees Dan confirmed, Erin waitlisted at position 1, and 0 seats remaining

    @negative
    Scenario: An attendee cannot see the full list
      Given a session with capacity 1 held by Dan with Erin waitlisted
      When Frank the attendee looks for the session's full registration list
      Then Frank is not shown other attendees' registrations
