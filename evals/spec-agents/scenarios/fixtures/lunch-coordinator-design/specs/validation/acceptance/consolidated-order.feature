Feature: The consolidated order and past rounds

  @story-7
  Rule: A closed round produces one consolidated order the whole team can read

    Scenario: The consolidated order groups items and totals the round
      Given Olivia has opened a round for "Bridge Cafe" holding Dan's 2 x "Falafel wrap" at 9.50 each and Priya's 1 x "Falafel wrap" at 9.50
      When Olivia closes the round
      Then the consolidated order shows "Falafel wrap" at a quantity of 3 and a grand total of 28.50

    Scenario: The consolidated order breaks the cost down per person
      Given Olivia has opened a round for "Bridge Cafe" holding Dan's 2 x "Falafel wrap" at 9.50 each and Priya's 1 x "Falafel wrap" at 9.50
      When Olivia closes the round
      Then the consolidated order shows Dan owing 19.00 and Priya owing 9.50

    Scenario: Any teammate reads the consolidated order, not only the opener
      Given Olivia has closed a round for "Bridge Cafe" holding Dan's "Falafel wrap" at 9.50
      When Priya opens that round
      Then she sees its consolidated order

    @negative
    Scenario: A round still open has no consolidated order
      Given Olivia has opened a round for "Bridge Cafe" at "12:30"
      When Dan opens that round
      Then no consolidated order is shown

  @story-8
  Rule: Past rounds stay readable as history

    Scenario: History lists a closed round with its restaurant and day
      Given Olivia has closed a round for "Bridge Cafe" today
      When Dan opens the history view
      Then he sees a "Bridge Cafe" round dated today

    Scenario: A past round still shows who ordered what
      Given Olivia has closed a round for "Bridge Cafe" holding Dan's "Falafel wrap"
      When Priya opens that round from the history view
      Then she sees "Falafel wrap" listed against Dan
