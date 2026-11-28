import type { ApiErrorBody, Envelope } from "./types";

// Every request is same-origin. The browser attaches the HttpOnly session
// cookie itself; no script ever sees the session token (decision 0013).

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

/** Fired when any read finds the browser session missing or revoked. */
export const UNAUTHENTICATED_EVENT = "conn:unauthenticated";

let csrfToken = "";

/** Set from GET /v1/web/session; sent on every unsafe request. */
export function setCsrfToken(token: string): void {
  csrfToken = token;
}

async function request<T>(method: string, path: string): Promise<Envelope<T> | undefined> {
  const headers: Record<string, string> = { Accept: "application/json" };
  if (method !== "GET" && method !== "HEAD") {
    headers["X-Conn-CSRF"] = csrfToken;
  }
  let response: Response;
  try {
    response = await fetch(path, {
      method,
      headers,
      credentials: "same-origin",
      cache: "no-store",
    });
  } catch {
    throw new ApiError(
      0,
      "network",
      "Conn could not be reached. Check your connection and try again.",
    );
  }
  if (response.status === 204) {
    return undefined;
  }
  const body = (await response.json().catch(() => undefined)) as
    | Envelope<T>
    | ApiErrorBody
    | undefined;
  if (!response.ok) {
    const detail = body && "error" in body ? body.error : undefined;
    const error = new ApiError(
      response.status,
      detail?.code ?? "unknown",
      detail?.message ?? `Conn answered HTTP ${response.status}.`,
    );
    if (response.status === 401 && path !== "/v1/web/session") {
      window.dispatchEvent(new Event(UNAUTHENTICATED_EVENT));
    }
    throw error;
  }
  if (!body || !("data" in body)) {
    throw new ApiError(
      response.status,
      "unreadable",
      "Conn returned a response the Lab cannot read.",
    );
  }
  return body;
}

export async function apiGet<T>(path: string): Promise<Envelope<T>> {
  const envelope = await request<T>("GET", path);
  if (!envelope) {
    throw new ApiError(204, "empty", "Conn returned no data.");
  }
  return envelope;
}

export async function apiDelete(path: string): Promise<void> {
  await request<never>("DELETE", path);
}
