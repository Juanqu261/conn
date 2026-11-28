import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect } from "react";
import { apiDelete, setCsrfToken, UNAUTHENTICATED_EVENT } from "./api/client";
import { sessionQuery } from "./api/queries";

/**
 * The signed-in Human of this browser. The session token stays in the
 * HttpOnly cookie; the Lab only ever holds the CSRF token.
 */
export function useSession() {
  const queryClient = useQueryClient();
  const session = useQuery(sessionQuery);

  useEffect(() => {
    if (session.data) {
      setCsrfToken(session.data.csrf_token);
    }
  }, [session.data]);

  // Any read that finds the session gone makes the session check run again,
  // which sends the viewer to sign-in.
  useEffect(() => {
    const recheck = () => {
      void queryClient.invalidateQueries({ queryKey: sessionQuery.queryKey });
    };
    window.addEventListener(UNAUTHENTICATED_EVENT, recheck);
    return () => window.removeEventListener(UNAUTHENTICATED_EVENT, recheck);
  }, [queryClient]);

  const signOut = useCallback(async () => {
    try {
      await apiDelete("/v1/web/session");
    } finally {
      setCsrfToken("");
      queryClient.clear();
      window.location.assign("/sign-in");
    }
  }, [queryClient]);

  return { session, signOut };
}

/** The sign-in URL that returns to path afterwards. */
export function signInHref(returnTo: string): string {
  return `/v1/auth/github/web/start?return_to=${encodeURIComponent(returnTo)}`;
}
