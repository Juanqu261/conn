-- Browser sessions for Conn Lab (decision 0013).
--
-- A session is either a CLI session (conn login, conn app, conn mcp, hooks)
-- presented as a bearer credential, or a browser session presented only in
-- the __Host-conn_web cookie. Neither kind is accepted where the other is
-- expected. Browser sessions live at most 7 days. The CSRF token is derived
-- from the session token on each request, so nothing else is stored.

-- +goose Up

ALTER TABLE sessions ADD COLUMN kind text NOT NULL DEFAULT 'cli'
    CONSTRAINT sessions_kind_check CHECK (kind IN ('cli', 'browser'));
ALTER TABLE sessions ADD CONSTRAINT sessions_browser_lifetime_check
    CHECK (kind <> 'browser' OR expires_at <= created_at + interval '7 days');

-- +goose Down
-- Development only. Automation never runs down migrations.

ALTER TABLE sessions DROP CONSTRAINT sessions_browser_lifetime_check;
ALTER TABLE sessions DROP COLUMN kind;
