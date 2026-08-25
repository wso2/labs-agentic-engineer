import { setupWorker } from "msw/browser";
import { activityHandlers } from "./handlers/activity";
import { agentChatHandlers } from "./handlers/agent-chat";
import { projectHandlers } from "./handlers/project";
import { projectsHandlers } from "./handlers/projects";
import { organizationsHandlers } from "./handlers/organizations";
import { settingsHandlers } from "./handlers/settings";
import { alertsHandlers } from "./handlers/alerts";
import { usageHandlers } from "./handlers/usage";

// Order matters: project-scoped routes (/projects/:name/...) are more
// specific than /projects/:name, so they register first.
export const worker = setupWorker(
  ...agentChatHandlers,
  ...activityHandlers,
  ...projectHandlers,
  ...projectsHandlers,
  ...organizationsHandlers,
  ...settingsHandlers,
  ...alertsHandlers,
  ...usageHandlers,
);
