screen MyAppointments "A patient's upcoming and past appointments"
  navbar "Clinic"
  sidebar "My appointments -> MyAppointments | Book a slot -> BrowseSlots | Sign out"
  heading "My appointments"
  row
    right
    button "Book a slot" primary -> BrowseSlots
  tabs "Upcoming | Past"
  table "Doctor | Date | Time | Status" -> RescheduleAppointment
    row "Dr. Rivera | 12 Oct | 09:30 | Booked"
    row "Dr. Chen | 02 Sep | 14:00 | Checked in"
  row
    button "Cancel appointment" danger // cancels in place; refused within 2 hours of start
    button "Reschedule" -> RescheduleAppointment

screen BrowseSlots "A patient finds an available slot"
  navbar "Clinic"
  sidebar "My appointments -> MyAppointments | Book a slot -> BrowseSlots | Sign out"
  heading "Available slots"
  row
    select "Doctor"
    input "Date"
    right
    search "Search"
  table "Doctor | Date | Time" -> MyAppointments
    row "Dr. Rivera | 12 Oct | 09:30"
    row "Dr. Rivera | 12 Oct | 10:00"
  button "Book selected slot" primary -> MyAppointments

screen RescheduleAppointment "A patient picks a new slot for an appointment"
  navbar "Clinic"
  sidebar "My appointments -> MyAppointments | Book a slot -> BrowseSlots | Sign out"
  heading "Reschedule appointment"
  text "Current: Dr. Rivera, 12 Oct 09:30"
  table "Doctor | Date | Time"
    row "Dr. Rivera | 13 Oct | 11:00"
    row "Dr. Rivera | 14 Oct | 09:30"
  row
    right
    button "Back" -> MyAppointments
    button "Confirm new slot" primary -> MyAppointments

screen DoctorSchedule "A doctor's appointments for the day"
  navbar "Clinic"
  sidebar "My day -> DoctorSchedule | My slots -> MySlots | Sign out"
  heading "My day"
  row
    input "Date"
    right
    button "Publish slots" primary -> PublishSlots
  table "Time | Patient | Status" -> VisitNote
    row "09:30 | Jane Doe | Checked in"
    row "10:00 | John Roe | Booked"

screen MySlots "A doctor's published slots"
  navbar "Clinic"
  sidebar "My day -> DoctorSchedule | My slots -> MySlots | Sign out"
  heading "My slots"
  input "Date"
  table "Date | Time | Status"
    row "12 Oct | 09:30 | Booked"
    row "12 Oct | 10:00 | Available"
  button "Remove selected slot" danger // removes in place; only unbooked slots

screen PublishSlots "A doctor publishes availability as slots"
  navbar "Clinic"
  sidebar "My day -> DoctorSchedule | My slots -> MySlots | Sign out"
  heading "Publish slots"
  input "Window start"
  input "Window end"
  select "Slot length (minutes)"
  row
    right
    button "Cancel" -> MySlots
    button "Publish" primary -> MySlots

screen VisitNote "A doctor writes the visit note for an appointment"
  navbar "Clinic"
  sidebar "My day -> DoctorSchedule | My slots -> MySlots | Sign out"
  heading "Visit note"
  text "Jane Doe, 12 Oct 09:30"
  badge "Checked in" success
  textarea "Visit notes (visible to doctors only)"
  row
    right
    button "Back" -> DoctorSchedule
    button "Save note" primary // saves in place

screen FrontDesk "A receptionist's appointments and check-in"
  navbar "Clinic"
  sidebar "Front desk -> FrontDesk | Book for patient -> BookForPatient | Sign out"
  heading "Front desk"
  row
    input "Date"
    select "Status"
    right
    button "Book for patient" primary -> BookForPatient
  table "Time | Patient | Doctor | Status"
    row "09:30 | Jane Doe | Dr. Rivera | Booked"
    row "10:00 | John Roe | Dr. Rivera | Checked in"
  row
    button "Check in" success // checks in in place; same-day only
    button "Reschedule"
    button "Cancel appointment" danger

screen BookForPatient "A receptionist books a slot for a patient"
  navbar "Clinic"
  sidebar "Front desk -> FrontDesk | Book for patient -> BookForPatient | Sign out"
  heading "Book for patient"
  input "Patient username"
  input "Patient name"
  row
    select "Doctor"
    input "Date"
  table "Doctor | Date | Time"
    row "Dr. Rivera | 12 Oct | 10:30"
  row
    right
    button "Back" -> FrontDesk
    button "Book slot" primary -> FrontDesk

flow "My appointments"
  role "Patient"
  description "A patient books, reschedules and cancels appointments"
  MyAppointments
  BrowseSlots
  RescheduleAppointment

flow "My day"
  role "Doctor"
  description "A doctor publishes slots, reviews the day and writes visit notes"
  DoctorSchedule
  VisitNote
  MySlots
  PublishSlots

flow "Front desk"
  role "Receptionist"
  description "A receptionist books for patients and checks them in"
  FrontDesk
  BookForPatient
