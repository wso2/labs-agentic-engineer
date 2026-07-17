import { setupWorker } from "msw/browser";
import { agentChatHandlers } from "./handlers/agent-chat";
import { projectHandlers } from "./handlers/project";
import { projectsHandlers } from "./handlers/projects";
import { organizationsHandlers } from "./handlers/organizations";
import { settingsHandlers } from "./handlers/settings";
import { alertsHandlers } from "./handlers/alerts";
import { modelsHandlers } from "./handlers/models";

// Order matters: project-scoped routes (/projects/:name/...) are more
// specific than /projects/:name, so they register first.
export const worker = setupWorker(
  ...agentChatHandlers,
  ...projectHandlers,
  ...projectsHandlers,
  ...organizationsHandlers,
  ...settingsHandlers,
  ...alertsHandlers,
  ...modelsHandlers,
);
