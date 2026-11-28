import {
  createRootRoute,
  createRoute,
  createRouter,
  Navigate,
  Outlet,
  useRouterState,
} from "@tanstack/react-router";
import { ApiError } from "./api/client";
import { parseBoardSearch } from "./board/filters";
import { BoltIcon } from "./components/icons";
import { Account } from "./routes/Account";
import { Blueprints } from "./routes/Blueprints";
import { CollisionBoard } from "./routes/CollisionBoard";
import { FeedPage } from "./routes/FeedPage";
import { Home } from "./routes/Home";
import { IntentBoard } from "./routes/IntentBoard";
import { NotFound } from "./routes/NotFound";
import { Overview } from "./routes/Overview";
import { ProjectLayout } from "./routes/ProjectLayout";
import { Pulse, type PulseWindow, WINDOWS } from "./routes/Pulse";
import { SignIn } from "./routes/SignIn";
import { Specimen, type SpecimenTab } from "./routes/Specimen";
import { type MapSearch, StitchMap } from "./routes/StitchMap";
import { StitchPage } from "./routes/StitchPage";
import { useSession } from "./session";
import { AppShell } from "./shell/AppShell";

// Every Lab view has a stable URL (requirement 3.6). Project URLs carry the
// Project ID behind a cosmetic name prefix: /p/checkout-reliability~<id>.

function Root() {
  const { session } = useSession();
  const location = useRouterState({ select: (state) => state.location });

  if (location.pathname === "/sign-in") {
    return <Outlet />;
  }
  if (session.isPending) {
    return (
      <div className="splash" role="status">
        <BoltIcon size={36} />
        <span>Charging the Lab…</span>
      </div>
    );
  }
  if (session.isError) {
    if (session.error instanceof ApiError && session.error.status === 401) {
      return <Navigate to="/sign-in" search={{ return_to: location.href }} replace />;
    }
    return (
      <div className="splash">
        <p>{session.error.message}</p>
        <button
          type="button"
          className="button button-secondary"
          onClick={() => void session.refetch()}
        >
          Try again
        </button>
      </div>
    );
  }
  return (
    <AppShell human={session.data.human}>
      <Outlet />
    </AppShell>
  );
}

const rootRoute = createRootRoute({ component: Root, notFoundComponent: NotFound });

const homeRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", component: Home });

const signInRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "sign-in",
  validateSearch: (
    search: Record<string, unknown>,
  ): { error?: string | undefined; return_to?: string | undefined } => ({
    error: typeof search.error === "string" ? search.error : undefined,
    return_to: typeof search.return_to === "string" ? search.return_to : undefined,
  }),
  component: SignIn,
});

const accountRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "account",
  component: Account,
});

const projectRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "p/$project",
  component: ProjectLayout,
});

const overviewRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/",
  validateSearch: (search: Record<string, unknown>): { focus?: string | undefined } => ({
    focus: typeof search.focus === "string" ? search.focus : undefined,
  }),
  component: Overview,
});

const feedRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "feed",
  component: FeedPage,
});

const boardRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "board",
  validateSearch: parseBoardSearch,
  component: IntentBoard,
});

const stitchesRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "stitches",
  component: CollisionBoard,
});

const stitchRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "stitches/$collision",
  component: StitchPage,
});

const SPECIMEN_TABS: SpecimenTab[] = ["overview", "claims", "stitches", "evidence", "review"];

const specimenRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "intents/$intent",
  validateSearch: (
    search: Record<string, unknown>,
  ): { tab?: SpecimenTab | undefined; revision?: number | undefined } => {
    const revision = Number(search.revision);
    return {
      tab: SPECIMEN_TABS.includes(search.tab as SpecimenTab)
        ? (search.tab as SpecimenTab)
        : undefined,
      revision: Number.isInteger(revision) && revision > 0 ? revision : undefined,
    };
  },
  component: Specimen,
});

const mapRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "map",
  validateSearch: (search: Record<string, unknown>): MapSearch => ({
    view: search.view === "table" ? "table" : undefined,
    subjects: search.subjects === true || search.subjects === "true" ? true : undefined,
    focus: typeof search.focus === "string" && search.focus ? search.focus : undefined,
  }),
  component: StitchMap,
});

const blueprintsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "blueprints",
  component: Blueprints,
});

const pulseRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "pulse",
  validateSearch: (search: Record<string, unknown>): { window?: PulseWindow | undefined } => {
    const value = Number(search.window);
    return {
      window: (WINDOWS as readonly number[]).includes(value) ? (value as PulseWindow) : undefined,
    };
  },
  component: Pulse,
});

const routeTree = rootRoute.addChildren([
  homeRoute,
  signInRoute,
  accountRoute,
  projectRoute.addChildren([
    overviewRoute,
    feedRoute,
    boardRoute,
    stitchesRoute,
    stitchRoute,
    specimenRoute,
    mapRoute,
    blueprintsRoute,
    pulseRoute,
  ]),
]);

export const router = createRouter({
  routeTree,
  defaultNotFoundComponent: NotFound,
  scrollRestoration: true,
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
