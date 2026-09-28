Feature: Personal todo management

  @story-1
  Rule: Only a signed-in user can access a todo list, and only their own

    Scenario: Signing in opens the user's own list
      Given Priya has an account
      When she signs in
      Then her own todo list is shown

    @negative
    Scenario: A user cannot see another user's todos
      Given Priya has a todo titled "Pay rent"
      When Dev signs in and opens his list
      Then "Pay rent" does not appear

  @story-2 @story-4
  Rule: A todo's title must be present and at most 120 characters

    @negative
    Scenario: Adding a todo with an empty title is refused
      Given Priya is signed in
      When she adds a todo with an empty title
      Then the todo is not added
      And she is told the title is required

    @negative
    Scenario: Adding a todo with a title over 120 characters is refused
      Given Priya is signed in
      When she adds a todo whose title is 121 characters long
      Then the todo is not added
