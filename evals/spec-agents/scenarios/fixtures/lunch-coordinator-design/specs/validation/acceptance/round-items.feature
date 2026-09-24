Feature: Items in a round

  @story-3
  Rule: A teammate may add items to the open round until its cutoff

    Scenario: Adding an item to the open round
      Given Olivia has opened a round for "Bridge Cafe" at "12:30" with no items
      When Dan adds "Falafel wrap", quantity 1, at 9.50
      Then the round holds exactly one item

    Scenario: Adding a second item to the same round
      Given Olivia has opened a round for "Bridge Cafe" at "12:30"
      And Dan has added "Falafel wrap", quantity 1, at 9.50
      When Dan adds "Mint tea", quantity 2, at 3.00
      Then the round holds exactly two items, both against Dan

    @negative
    Scenario: An item is refused once the round has passed its cutoff
      Given Olivia has opened a round for "Bridge Cafe" that has passed its cutoff with no items
      When Dan tries to add "Falafel wrap", quantity 1, at 9.50
      Then the round still holds no items

  @story-4
  Rule: A teammate may change only their own items, and only while the round is open

    Scenario: Editing your own item before cutoff
      Given Olivia has opened a round for "Bridge Cafe" at "12:30"
      And Dan has added "Falafel wrap", quantity 1, at 9.50
      When Dan changes that item to quantity 2
      Then the round holds exactly one item, of quantity 2

    Scenario: Removing your own item before cutoff
      Given Olivia has opened a round for "Bridge Cafe" at "12:30"
      And Dan has added "Falafel wrap", quantity 1, at 9.50
      When Dan removes that item
      Then the round holds no items

    @negative
    Scenario: A teammate cannot edit somebody else's item
      Given Olivia has opened a round for "Bridge Cafe" at "12:30"
      And Dan has added "Falafel wrap", quantity 1, at 9.50
      When Priya tries to change Dan's "Falafel wrap" to quantity 5
      Then that item is still quantity 1

    @negative
    Scenario: A teammate cannot remove somebody else's item
      Given Olivia has opened a round for "Bridge Cafe" at "12:30"
      And Dan has added "Falafel wrap", quantity 1, at 9.50
      When Priya tries to remove Dan's "Falafel wrap"
      Then the round still holds exactly one item

  @story-5
  Rule: A closed round's items can no longer be changed

    @negative
    Scenario: An item cannot be edited once the round has passed its cutoff
      Given Olivia has opened a round for "Bridge Cafe" holding Dan's "Falafel wrap" at quantity 1
      And that round has passed its cutoff
      When Dan tries to change that item to quantity 2
      Then that item is still quantity 1

    @negative
    Scenario: An item cannot be removed once the round has passed its cutoff
      Given Olivia has opened a round for "Bridge Cafe" holding Dan's "Falafel wrap" at quantity 1
      And that round has passed its cutoff
      When Dan tries to remove that item
      Then the round still holds exactly one item
