Feature: Approvals

  @story-4
  Rule: An approver can see pending requests

    Scenario: Pending request appears in the queue
      Given Rita has raised a pending request
      When Alan the approver opens the approval queue
      Then Rita's request is listed as pending

  @story-5
  Rule: An approver can approve or reject a pending request whose total is within the approval limit of 5000

    Scenario: Approving within the limit
      Given Rita has raised a pending request with a total of 4000
      When Alan approves it
      Then the request shows as approved

    Scenario: Rejecting a request
      Given Rita has raised a pending request
      When Alan rejects it with the reason "Not budgeted"
      Then the request shows as rejected

  @story-6
  Rule: No approver can approve a request whose total is above 5000

    @negative
    Scenario: Request above the limit
      Given Rita has raised a pending request with a total of 6000
      When Alan the approver tries to approve it
      Then the request is still pending

  @story-5
  Rule: An approver cannot approve their own request

    @negative
    Scenario: Self-approval
      Given Alan the approver has raised a pending request
      When Alan tries to approve it
      Then the request is still pending
