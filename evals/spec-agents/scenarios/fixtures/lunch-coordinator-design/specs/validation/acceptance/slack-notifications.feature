Feature: Slack notifications

  @story-9
  Rule: The team's Slack channel is told when a round opens and when it reaches cutoff

    Scenario: Opening a round posts to the team channel
      Given no round is open
      When Olivia opens a round for "Bridge Cafe" at "12:30"
      Then a message naming "Bridge Cafe" is posted to the team's Slack channel

    Scenario: Reaching the cutoff posts to the team channel
      Given Olivia has opened a round for "Bridge Cafe" with a cutoff one minute from now
      When the cutoff time passes
      Then a message for that round is posted to the team's Slack channel
