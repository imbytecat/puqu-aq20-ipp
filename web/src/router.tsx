import { createRootRoute, createRoute, createRouter } from "@tanstack/react-router";
import {
  DashboardPage,
  DevicesPage,
  JobsPage,
  PrinterPage,
  PrintersPage,
  ProfilesPage,
  RootLayout,
  RuntimePage,
} from "./App";

const rootRoute = createRootRoute({ component: RootLayout });
const dashboardRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", component: DashboardPage });
const printersRoute = createRoute({ getParentRoute: () => rootRoute, path: "/printers", component: PrintersPage });
const printerRoute = createRoute({ getParentRoute: () => rootRoute, path: "/printers/$printerId", component: PrinterPage });
const devicesRoute = createRoute({ getParentRoute: () => rootRoute, path: "/devices", component: DevicesPage });
const profilesRoute = createRoute({ getParentRoute: () => rootRoute, path: "/profiles", component: ProfilesPage });
const jobsRoute = createRoute({ getParentRoute: () => rootRoute, path: "/jobs", component: JobsPage });
const runtimeRoute = createRoute({ getParentRoute: () => rootRoute, path: "/runtime", component: RuntimePage });

const routeTree = rootRoute.addChildren([
  dashboardRoute,
  printersRoute,
  printerRoute,
  devicesRoute,
  profilesRoute,
  jobsRoute,
  runtimeRoute,
]);

export const router = createRouter({ routeTree, defaultPreload: "intent" });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
