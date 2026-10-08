Feature: Purchase requests

  @story-1
  Rule: A requester can browse the catalogue

    Scenario: Browsing the catalogue
      Given Rita the requester is signed in
      When she opens the catalogue
      Then she sees catalogue items with their names and unit prices

  @story-2
  Rule: A purchase request holds catalogue items with quantities and starts pending

    Scenario: Raising a request
      Given Rita the requester is signed in
      When she raises a request for 10 units of a catalogue item
      Then her new request is pending
      And its total value is 10 times the item's unit price

    @negative
    Scenario: A request with no items is refused
      Given Rita the requester is signed in
      When she tries to raise a request with no items
      Then her list of requests has no new request

  @story-3
  Rule: A requester sees the status of only their own requests

    Scenario: Following a request
      Given Rita has raised a request that Alan the approver then rejected
      When she opens her requests
      Then that request shows as rejected

    @negative
    Scenario: Another requester's requests are not visible
      Given Rita has raised a request
      When Ravi, another requester, opens his requests
      Then Rita's request is not among them
