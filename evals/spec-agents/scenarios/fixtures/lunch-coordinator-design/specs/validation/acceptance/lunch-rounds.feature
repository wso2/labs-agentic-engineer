Feature: Lunch rounds

  @story-1
  Rule: Any signed-in teammate may open the day's round

    Scenario: Opening the day's round
      Given no round is open
      When Olivia opens a round for "Bridge Cafe" at "12:30"
      Then the round is open for "Bridge Cafe" with a cutoff of "12:30"

    Scenario: Opening a round with a note for the team
      Given no round is open
      When Olivia opens a round for "Bridge Cafe" at "12:30" with the note "Pickup, not delivery"
      Then the open round shows the note "Pickup, not delivery"

    @negative
    Scenario: Someone who has not signed in reaches no round
      Given Priya has not signed in
      When Priya opens the app
      Then she is taken to the Google Workspace sign-in before any round is shown

  @story-1
  Rule: Only one round may be open at a time

    @negative
    Scenario: A second round is refused while one is open
      Given Olivia has opened a round for "Bridge Cafe" at "12:30"
      When Dan tries to open a round for "Riverside Kitchen" at "13:00"
      Then exactly one round is open, and it is Olivia's "Bridge Cafe" round

  @story-2
  Rule: Any signed-in teammate can see the open round and everything added to it

    Scenario: The open round shows its restaurant and cutoff
      Given Olivia has opened a round for "Bridge Cafe" at "12:30"
      When Dan opens the round
      Then he sees "Bridge Cafe" and a cutoff of "12:30"

    Scenario: The open round lists each item against whoever added it
      Given Olivia has opened a round for "Bridge Cafe" at "12:30"
      And Dan has added "Falafel wrap" to that round
      When Priya opens the round
      Then she sees "Falafel wrap" listed against Dan

  @story-5
  Rule: A round closes itself once its cutoff passes

    Scenario: The round closes at its cutoff
      Given Olivia has opened a round for "Bridge Cafe" with a cutoff one minute from now
      When the cutoff time passes
      Then the round is closed

  @story-6
  Rule: The opener may close their round before the cutoff

    Scenario: The opener closes the round early
      Given Olivia has opened a round for "Bridge Cafe" at "12:30"
      When Olivia closes the round
      Then the round is closed

    @negative
    Scenario: A teammate who did not open the round cannot close it
      Given Olivia has opened a round for "Bridge Cafe" at "12:30"
      When Dan tries to close that round
      Then the round is still open
