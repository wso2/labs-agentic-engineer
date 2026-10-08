Feature: Catalogue management

  @story-11
  Rule: A warehouse clerk can add items, which start with 0 in stock

    Scenario: Adding an item
      Given Wanda the warehouse clerk is signed in
      When she adds an item named uniquely for this run with unit "box", unit price 12.50 and reorder level 5
      Then the item appears in the catalogue with unit "box" and unit price 12.50
      And the item has 0 units on hand

    @negative
    Scenario: A requester cannot add items
      Given Rita the requester is signed in
      When she tries to add an item named uniquely for this run
      Then the catalogue has no new item

  @story-12
  Rule: A warehouse clerk can edit an item's name, unit, unit price and reorder level

    Scenario: Editing the unit price and reorder level
      Given Wanda has added an item with unit price 12.50 and reorder level 5
      When she changes its unit price to 14.00 and its reorder level to 8
      Then the item shows unit price 14.00 and reorder level 8

    @negative
    Scenario: A requester cannot edit items
      Given Wanda has added an item with unit price 12.50
      When Rita the requester tries to change its unit price to 1.00
      Then the item still shows unit price 12.50
