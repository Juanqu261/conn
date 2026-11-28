import {
  createRootRoute,
  createRoute,
  createRouter,
  Navigate,
  Outlet,
  useRouterState,
} from "@tanstack/react-router";
import { ApiError } from "./api/client";
import { BoltIcon } from "./components/icons";
import { Account } from "./routes/Account";
import { FeedPage } from "./routes/FeedPage";
import { Home } from "./routes/Home";
import { NotFound } from "./routes/NotFound";
import { Overview } from "./routes/Overview";
import { ProjectLayout } from "./routes/ProjectLayout";
import { SignIn } from "./routes/SignIn";
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

const routeTree = rootRoute.addChildren([
  homeRoute,
  signInRoute,
  accountRoute,
  projectRoute.addChildren([overviewRoute, feedRoute]),
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
