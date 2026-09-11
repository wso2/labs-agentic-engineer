import { setupWorker } from "msw/browser";
import { agentChatHandlers } from "./handlers/agent-chat";
import { buildHandlers } from "./handlers/build";
import { projectHandlers } from "./handlers/project";
import { projectsHandlers } from "./handlers/projects";
import { organizationsHandlers } from "./handlers/organizations";
import { settingsHandlers } from "./handlers/settings";
import { marketplaceHandlers } from "./handlers/marketplace";
import { alertsHandlers } from "./handlers/alerts";
import { usageHandlers } from "./handlers/usage";
import { workloadDependenciesHandlers } from "./handlers/workload-dependencies";
import { resourcesHandlers } from "./handlers/resources";
import { rolesHandlers } from "./handlers/roles";

// Order matters: project-scoped routes (/projects/:name/...) are more
// specific than /projects/:name, so they register first.
export const worker = setupWorker(
  ...agentChatHandlers,
  ...buildHandlers,
  ...workloadDependenciesHandlers,
  ...projectHandlers,
  ...projectsHandlers,
  ...organizationsHandlers,
  ...settingsHandlers,
  ...marketplaceHandlers,
  ...resourcesHandlers,
  ...rolesHandlers,
  ...alertsHandlers,
  ...usageHandlers,
);
