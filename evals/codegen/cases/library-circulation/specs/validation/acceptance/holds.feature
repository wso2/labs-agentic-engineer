Feature: Holds

  @story-4
  Rule: A member may place a hold only on a title with no available copy

    Scenario: Placing a hold
      Given a title has one copy and it is on loan to someone else
      When Mia the member places a hold on the title
      Then Mia has a waiting hold on it

    @negative
    Scenario: A title with an available copy cannot be held
      Given a title has one available copy
      When Mia the member tries to place a hold on it
      Then Mia has no hold on it

    @negative
    Scenario: A second hold on the same title is refused
      Given Mia the member has a waiting hold on a title with no available copy
      When she tries to place another hold on it
      Then Mia has exactly one hold on that title

  @story-5
  Rule: A member may cancel only their own hold

    Scenario: Cancelling a hold
      Given Mia the member has a waiting hold on a title
      When she cancels it
      Then the title's queue no longer contains Mia

    @negative
    Scenario: A member cannot cancel another member's hold
      Given Dan the member has a waiting hold on a title
      When Mia the member tries to cancel Dan's hold
      Then Dan's hold is still waiting

  @story-13
  Rule: Only a librarian may view and work the holds queue

    Scenario: Viewing the queue in order
      Given Mia placed a hold on a title before Dan did
      When Lena the librarian opens the holds queue for that title
      Then Mia is listed ahead of Dan

    Scenario: A librarian cancels a hold
      Given Dan has a waiting hold on a title
      When Lena the librarian cancels Dan's hold
      Then the queue for that title is empty

    @negative
    Scenario: A member cannot see the holds queue of all members
      Given Mia is signed in as a member
      When she tries to open the holds queue for a title
      Then no other member's holds are shown
