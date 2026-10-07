Feature: Sign-in and role access

  @story-13
  Rule: Users must sign in and see only what their role allows

    @negative
    Scenario: Visitor who is not signed in
      Given a visitor is not signed in
      When the visitor opens the clinic app
      Then no appointments or slots are shown

    Scenario: Receptionist sees front-desk tools
      Given Rita the receptionist is signed in
      When she opens the clinic app
      Then she can see check-in for patients

    @negative
    Scenario: A patient is not offered front-desk or doctor tools
      Given Pat the patient is signed in
      When she opens the clinic app
      Then she cannot reach the check-in or publish-slots screens
