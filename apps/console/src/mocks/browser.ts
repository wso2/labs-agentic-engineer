import { setupWorker } from "msw/browser";
import { activityHandlers } from "./handlers/activity";
import { agentChatHandlers } from "./handlers/agent-chat";
import { projectHandlers } from "./handlers/project";
import { projectsHandlers } from "./handlers/projects";
import { organizationsHandlers } from "./handlers/organizations";
import { settingsHandlers } from "./handlers/settings";
import { alertsHandlers } from "./handlers/alerts";
import { deploymentsHandlers } from "./handlers/deployments";
import { usageHandlers } from "./handlers/usage";

// Order matters: project-scoped routes (/projects/:name/...) are more
// specific than /projects/:name, so they register first.
export const worker = setupWorker(
  ...agentChatHandlers,
  ...activityHandlers,
  // Before projectHandlers: /projects/:name/deployments must not be swallowed
  // by a broader project-scoped matcher registered earlier.
  ...deploymentsHandlers,
  ...projectHandlers,
  ...projectsHandlers,
  ...organizationsHandlers,
  ...settingsHandlers,
  ...alertsHandlers,
  ...usageHandlers,
);
