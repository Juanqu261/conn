import "@fontsource-variable/inter";
import "@fontsource-variable/jetbrains-mono";
import "@fontsource/creepster";
import "./theme/tokens.css";
import "./styles/app.css";
import "./styles/board.css";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { ApiError } from "./api/client";
import { router } from "./router";
import { applyTheme } from "./theme/preferences";

applyTheme();

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // The event stream invalidates what changed; there is no timer polling.
      staleTime: 30_000,
      refetchOnWindowFocus: false,
      retry: (failures, error) =>
        !(error instanceof ApiError && error.status >= 400 && error.status < 500) && failures < 2,
    },
  },
});

const root = document.getElementById("root");
if (root) {
  createRoot(root).render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </StrictMode>,
  );
}
