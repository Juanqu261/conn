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

async function send(method: string, path: string, body?: unknown, accept = "application/json") {
  const headers: Record<string, string> = { Accept: accept };
  if (method !== "GET" && method !== "HEAD") {
    headers["X-Conn-CSRF"] = csrfToken;
  }
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
  }
  try {
    return await fetch(path, {
      method,
      headers,
      credentials: "same-origin",
      cache: "no-store",
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    });
  } catch {
    throw new ApiError(
      0,
      "network",
      "Conn could not be reached. Check your connection and try again.",
    );
  }
}

async function failure(response: Response, path: string): Promise<ApiError> {
  const body = (await response.json().catch(() => undefined)) as ApiErrorBody | undefined;
  const detail = body && "error" in body ? body.error : undefined;
  if (response.status === 401 && path !== "/v1/web/session") {
    window.dispatchEvent(new Event(UNAUTHENTICATED_EVENT));
  }
  return new ApiError(
    response.status,
    detail?.code ?? "unknown",
    detail?.message ?? `Conn answered HTTP ${response.status}.`,
  );
}

async function request<T>(
  method: string,
  path: string,
  payload?: unknown,
): Promise<Envelope<T> | undefined> {
  const response = await send(method, path, payload);
  if (response.status === 204) {
    return undefined;
  }
  if (!response.ok) {
    throw await failure(response, path);
  }
  const body = (await response.json().catch(() => undefined)) as Envelope<T> | undefined;
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

/**
 * Records a human decision. The browser adds Origin itself; the CSRF token
 * travels in X-Conn-CSRF. The Lab never retries an unsafe request.
 */
export async function apiPost<T>(path: string, body?: unknown): Promise<Envelope<T>> {
  const envelope = await request<T>("POST", path, body);
  if (!envelope) {
    throw new ApiError(204, "empty", "Conn returned no data.");
  }
  return envelope;
}

/** Reads a generated text document, such as review packet Markdown. */
export async function apiGetText(path: string): Promise<{ text: string; asOfEvent: number }> {
  const response = await send("GET", path, undefined, "text/markdown");
  if (!response.ok) {
    throw await failure(response, path);
  }
  return {
    text: await response.text(),
    asOfEvent: Number(response.headers.get("X-Conn-As-Of-Event") ?? 0),
  };
}
