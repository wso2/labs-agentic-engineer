screen Dashboard "HR Coordinator's cross-department view of every active onboarding"
  navbar "Onboarding Tracker"
  sidebar "Dashboard -> Dashboard | New Onboarding -> NewOnboarding | HR Tasks -> HrTasks | Task Templates -> TaskTemplates"
  row
    heading "Active Onboardings"
    right
    button "New Onboarding" primary -> NewOnboarding
  table "New Hire | Start Date | IT | HR | Facilities | Overdue | Status" -> OnboardingDetail
    row "Jane Doe | 2024-06-03 | 2/3 | 1/2 | 3/3 | Yes | In Progress"
    row "Sam Lee | 2024-06-10 | 3/3 | 2/2 | 2/2 | No | In Progress"
    row "Priya Nair | 2024-05-20 | 3/3 | 2/2 | 2/2 | No | Complete"

screen NewOnboarding "HR Coordinator starts a new hire's onboarding"
  navbar "Onboarding Tracker"
  sidebar "Dashboard -> Dashboard | New Onboarding -> NewOnboarding | HR Tasks -> HrTasks | Task Templates -> TaskTemplates"
  heading "New Onboarding"
  input "New hire name"
  input "Start date"
  text "Tasks will be seeded automatically from the standard IT, HR and Facilities templates."
  row
    right
    button "Cancel" -> Dashboard
    button "Create" primary -> OnboardingDetail

screen OnboardingDetail "One new hire's full onboarding checklist, by department"
  navbar "Onboarding Tracker"
  sidebar "Dashboard -> Dashboard | New Onboarding -> NewOnboarding | HR Tasks -> HrTasks | Task Templates -> TaskTemplates"
  row
    heading "Jane Doe"
    right
    badge "In Progress" warning
  text "Start date: 2024-06-03"
  row
    right
    button "Add Task" -> AddTask
    button "Mark Complete" primary
  table "Department | Task | Due Date | Status"
    row "IT | Provision laptop | 2024-06-01 | Overdue"
    row "IT | Create email account | 2024-05-30 | Complete"
    row "HR | Send benefits packet | 2024-06-02 | In Progress"
    row "Facilities | Assign desk | 2024-05-29 | Complete"

screen AddTask "HR Coordinator adds an exception task to a record"
  navbar "Onboarding Tracker"
  heading "Add Task"
  select "Department (IT / HR / Facilities)"
  input "Task name"
  input "Due date"
  textarea "Notes"
  row
    right
    button "Cancel" -> OnboardingDetail
    button "Save" primary -> OnboardingDetail

screen ItTasks "IT Staff's queue of tasks across all new hires"
  navbar "Onboarding Tracker"
  sidebar "IT Tasks -> ItTasks"
  row
    heading "IT Tasks"
    right
    select "Status: All"
  table "New Hire | Task | Due Date | Status"
    row "Jane Doe | Provision laptop | 2024-06-01 | Overdue"
    row "Sam Lee | Create accounts | 2024-06-08 | Pending"
    row "Priya Nair | Provision laptop | 2024-05-18 | Complete"

screen FacilitiesTasks "Facilities Staff's queue of tasks across all new hires"
  navbar "Onboarding Tracker"
  sidebar "Facilities Tasks -> FacilitiesTasks"
  row
    heading "Facilities Tasks"
    right
    select "Status: All"
  table "New Hire | Task | Due Date | Status"
    row "Jane Doe | Assign desk | 2024-06-01 | Complete"
    row "Sam Lee | Issue badge | 2024-06-09 | Pending"

screen HrTasks "HR Coordinator's own department task queue"
  navbar "Onboarding Tracker"
  sidebar "Dashboard -> Dashboard | New Onboarding -> NewOnboarding | HR Tasks -> HrTasks | Task Templates -> TaskTemplates"
  row
    heading "HR Tasks"
    right
    select "Status: All"
  table "New Hire | Task | Due Date | Status"
    row "Jane Doe | Send benefits packet | 2024-06-02 | In Progress"
    row "Sam Lee | Schedule orientation | 2024-06-11 | Pending"

screen TaskTemplates "Admin manages the standard per-department checklist"
  navbar "Onboarding Tracker"
  sidebar "Dashboard -> Dashboard | Task Templates -> TaskTemplates"
  row
    heading "Task Templates"
    right
    button "New Template" primary -> EditTemplate
  table "Department | Task Name | Default Due Offset" -> EditTemplate
    row "IT | Provision laptop | -3 days"
    row "IT | Create email account | -5 days"
    row "HR | Send benefits packet | -2 days"
    row "Facilities | Assign desk | -3 days"

screen EditTemplate "Admin creates or edits one task template"
  navbar "Onboarding Tracker"
  heading "Task Template"
  select "Department (IT / HR / Facilities)"
  input "Task name"
  input "Default due offset (days before start)"
  row
    right
    button "Cancel" -> TaskTemplates
    button "Save" primary -> TaskTemplates

flow "Manage onboarding"
  role "HR Coordinator"
  description "Create onboarding records, handle exceptions, watch the dashboard, close out onboarding"
  Dashboard
  NewOnboarding
  OnboardingDetail
  AddTask
  HrTasks

flow "IT task queue"
  role "IT Staff"
  description "Work through IT's onboarding tasks across all new hires"
  ItTasks

flow "Facilities task queue"
  role "Facilities Staff"
  description "Work through Facilities' onboarding tasks across all new hires"
  FacilitiesTasks

flow "Template management"
  role "Admin"
  description "Keep the standard per-department task templates current"
  TaskTemplates
  EditTemplate
