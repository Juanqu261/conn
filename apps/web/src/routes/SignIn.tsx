import { useQuery } from "@tanstack/react-query";
import { Navigate, useSearch } from "@tanstack/react-router";
import { labStatusQuery, sessionQuery } from "../api/queries";
import { BoltIcon } from "../components/icons";
import { useDocumentTitle } from "../components/title";
import { signInHref } from "../session";

const ERRORS: Record<string, string> = {
  denied: "GitHub sign-in was cancelled. Nothing was changed.",
  state_mismatch: "That sign-in link was not started in this browser, or it expired. Start again.",
  code_rejected: "GitHub did not accept the sign-in. Start again.",
  github_unavailable: "GitHub could not be reached to confirm who you are. Try again in a moment.",
  not_configured: "This Conn server cannot sign browsers in yet.",
  failed: "Sign-in did not complete. Start again.",
};

const SETTING_HINTS: Record<string, string> = {
  CONN_GITHUB_CLIENT_ID: "the GitHub OAuth client ID that conn login uses",
  CONN_GITHUB_CLIENT_SECRET: "a client secret generated for that same OAuth client",
  CONN_PUBLIC_URL: "the origin you open the Lab at, such as http://127.0.0.1:8080",
};

/** Before sign-in the Lab shows only this page (requirement 2.1). */
export function SignIn() {
  const { error, return_to } = useSearch({ from: "/sign-in" });
  const session = useQuery(sessionQuery);
  const lab = useQuery(labStatusQuery);
  useDocumentTitle("Sign in");

  if (session.data) {
    return <Navigate to="/" replace />;
  }

  const unavailable = lab.data && !lab.data.sign_in_available;
  const returnTo = return_to?.startsWith("/") && !return_to.startsWith("//") ? return_to : "/";

  return (
    <main className="signin">
      <div className="signin-card">
        <span className="signin-bolt" aria-hidden="true">
          <BoltIcon size={40} />
        </span>
        <h1 className="signin-title">
          <span className="wordmark-large">Conn Lab</span>
        </h1>
        <p className="signin-tagline">
          It's alive. See what your team is stitching together, where it collides, and what is
          proven.
        </p>

        {error && ERRORS[error] && (
          <p className="notice notice-warning" role="alert">
            {ERRORS[error]}
          </p>
        )}

        {unavailable ? (
          <div className="notice notice-failing" role="alert">
            <p>Browser sign-in is not configured on this Conn server. It still needs:</p>
            <ul>
              {lab.data?.missing_settings.map((setting) => (
                <li key={setting}>
                  <code className="mono">{setting}</code>: {SETTING_HINTS[setting]}
                </li>
              ))}
            </ul>
            <p>
              Set them where conn server runs and restart it. The terminal workflow is unaffected.
            </p>
          </div>
        ) : (
          <a className="button button-primary button-large" href={signInHref(returnTo)}>
            Sign in with GitHub
          </a>
        )}

        <p className="signin-fine">
          Conn asks GitHub only who you are. It never sees your code, and the Lab shows only
          Projects you belong to.
        </p>

        {lab.data?.sign_in_available && (
          <details className="signin-help">
            <summary>
              GitHub says “The redirect_uri is not associated with this application”?
            </summary>
            <p>
              The GitHub OAuth app's <strong>Authorization callback URL</strong> must be exactly:
            </p>
            <code className="mono signin-callback">
              {lab.data.lab_url ?? window.location.origin}/v1/auth/github/web/callback
            </code>
            <p>
              Change it under GitHub › Settings › Developer settings › OAuth Apps, then sign in
              again.
            </p>
          </details>
        )}
      </div>
    </main>
  );
}
