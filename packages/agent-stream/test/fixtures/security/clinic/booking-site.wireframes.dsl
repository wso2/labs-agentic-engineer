screen FindASlot "Open times this week, before anyone signs in"
  navbar "City Clinic"
  table "Day | Time | Doctor" -> MyAppointments
    row "Mon | 09:30 | Dr Silva"

screen MyAppointments "Appointments I have booked"
  navbar "City Clinic"
  table "Day | Time | Doctor"
    row "Mon | 09:30 | Dr Silva"
  button "Book another" primary -> FindASlot
