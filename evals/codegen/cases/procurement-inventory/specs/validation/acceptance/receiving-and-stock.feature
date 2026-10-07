Feature: Receiving and stock

  @story-7 @story-8
  Rule: Receiving goods against an approved order raises stock and writes a ledger entry

    Scenario: Receiving an approved order
      Given an approved order for 10 units of a catalogue item with 5 units on hand
      When Wanda the warehouse clerk receives 10 units
      Then the item has 15 units on hand
      And the item's ledger has one new entry of plus 10

  @story-7
  Rule: Goods can only be received against an approved order, up to the outstanding quantity

    @negative
    Scenario: Pending order cannot be received
      Given a pending order for 10 units of a catalogue item with 5 units on hand
      When Wanda tries to receive 10 units
      Then the item still has 5 units on hand

    @negative
    Scenario: Receiving more than ordered
      Given an approved order for 10 units of a catalogue item with 5 units on hand
      When Wanda tries to receive 11 units
      Then the item still has 5 units on hand

  @story-7
  Rule: Receiving may be partial and the order stays open until fully received

    Scenario: Partial receipt
      Given an approved order for 10 units of a catalogue item
      When Wanda receives 4 units
      Then the order shows as partially received with 6 units outstanding

  @story-9
  Rule: A clerk can view an item's ledger

    Scenario: Viewing the ledger
      Given an item has had a receipt of 10 units recorded
      When Wanda opens that item's ledger
      Then the ledger shows an entry of plus 10 with the balance after it

  @story-10
  Rule: The low-stock view lists only items below their reorder level

    Scenario: Item below its reorder level
      Given an item with 3 units on hand and a reorder level of 10
      When Wanda opens the low-stock view
      Then that item is listed

    @negative
    Scenario: Item at its reorder level
      Given an item with 10 units on hand and a reorder level of 10
      When Wanda opens the low-stock view
      Then that item is not listed

  @story-7 @story-9
  Rule: Only a warehouse clerk may receive goods or view stock

    @negative
    Scenario: A requester cannot receive goods
      Given an approved order for 10 units of a catalogue item with 5 units on hand
      When Rita the requester tries to receive 10 units
      Then the item still has 5 units on hand
