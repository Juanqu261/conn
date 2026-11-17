-- Conn backend schema for the first vertical slice: humans and sessions,
-- projects and memberships, objective and blueprint revisions, cell
-- projections, charter decisions, and project events.
--
-- Constraints here are defense in depth. The application validates every
-- rule first; the database guarantees that no committed row breaks
-- identity, ownership, revision order, own-charter authority, append-only
-- history, or gap-free event sequences.

-- +goose Up

-- +goose StatementBegin
CREATE FUNCTION conn_reject_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'rows in % are append-only', TG_TABLE_NAME
        USING ERRCODE = 'integrity_constraint_violation';
END;
$$;
-- +goose StatementEnd

CREATE TABLE humans (
    human_id       uuid        PRIMARY KEY CHECK (uuid_extract_version(human_id) = 7),
    github_user_id bigint      NOT NULL UNIQUE CHECK (github_user_id > 0),
    github_login   text        NOT NULL CHECK (char_length(github_login) BETWEEN 1 AND 39),
    created_at     timestamptz NOT NULL,
    updated_at     timestamptz NOT NULL,
    CHECK (updated_at >= created_at)
);

-- Only SHA-256 hashes of opaque session tokens are stored (decision 0021).
CREATE TABLE sessions (
    session_id   uuid        PRIMARY KEY CHECK (uuid_extract_version(session_id) = 7),
    human_id     uuid        NOT NULL REFERENCES humans,
    token_sha256 bytea       NOT NULL UNIQUE CHECK (octet_length(token_sha256) = 32),
    created_at   timestamptz NOT NULL,
    expires_at   timestamptz NOT NULL,
    revoked_at   timestamptz,
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '30 days'),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);
CREATE INDEX sessions_human_id_idx ON sessions (human_id);

CREATE TABLE projects (
    project_id     uuid        PRIMARY KEY CHECK (uuid_extract_version(project_id) = 7),
    name           text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    owner_human_id uuid        NOT NULL REFERENCES humans,
    -- Constant used by the ownership foreign key below.
    owner_role     text        NOT NULL GENERATED ALWAYS AS ('owner') STORED,
    created_at     timestamptz NOT NULL
);

CREATE TABLE project_memberships (
    project_id        uuid        NOT NULL REFERENCES projects,
    human_id          uuid        NOT NULL REFERENCES humans,
    role              text        NOT NULL CHECK (role IN ('owner', 'member')),
    added_by_human_id uuid        NOT NULL,
    -- Constant used by the adder foreign key below.
    adder_role        text        NOT NULL GENERATED ALWAYS AS ('owner') STORED,
    added_at          timestamptz NOT NULL,
    PRIMARY KEY (project_id, human_id),
    UNIQUE (project_id, human_id, role),
    -- Only a project owner adds members; the owner's own row references itself.
    FOREIGN KEY (project_id, added_by_human_id, adder_role)
        REFERENCES project_memberships (project_id, human_id, role)
        DEFERRABLE INITIALLY DEFERRED
);
CREATE UNIQUE INDEX project_memberships_one_owner_idx
    ON project_memberships (project_id) WHERE role = 'owner';
CREATE INDEX project_memberships_human_id_idx ON project_memberships (human_id);

-- The recorded owner must hold the owner membership by the end of the
-- creating transaction.
ALTER TABLE projects
    ADD FOREIGN KEY (project_id, owner_human_id, owner_role)
        REFERENCES project_memberships (project_id, human_id, role)
        DEFERRABLE INITIALLY DEFERRED;

-- Current Objective revision per Objective.
CREATE TABLE objectives (
    objective_id     uuid    PRIMARY KEY CHECK (uuid_extract_version(objective_id) = 7),
    project_id       uuid    NOT NULL REFERENCES projects,
    current_revision integer NOT NULL CHECK (current_revision > 0),
    UNIQUE (project_id, objective_id)
);

-- Immutable Objective revisions. document is the canonical protocol record.
CREATE TABLE objective_revisions (
    objective_id         uuid        NOT NULL,
    revision             integer     NOT NULL CHECK (revision > 0),
    -- Revision n requires revision n - 1, so revisions are contiguous.
    previous_revision    integer     GENERATED ALWAYS AS (NULLIF(revision - 1, 0)) STORED,
    project_id           uuid        NOT NULL,
    accountable_human_id uuid        NOT NULL,
    created_at           timestamptz NOT NULL,
    document             jsonb       NOT NULL,
    PRIMARY KEY (objective_id, revision),
    FOREIGN KEY (objective_id, previous_revision) REFERENCES objective_revisions (objective_id, revision),
    FOREIGN KEY (project_id, objective_id) REFERENCES objectives (project_id, objective_id),
    FOREIGN KEY (project_id, accountable_human_id) REFERENCES project_memberships (project_id, human_id),
    CHECK (jsonb_typeof(document) = 'object'),
    CHECK (document -> 'objective_id' = to_jsonb(objective_id::text)),
    CHECK (document -> 'project_id' = to_jsonb(project_id::text)),
    CHECK (document -> 'revision' = to_jsonb(revision)),
    CHECK (document -> 'accountable_human_id' = to_jsonb(accountable_human_id::text))
);

ALTER TABLE objectives
    ADD FOREIGN KEY (objective_id, current_revision)
        REFERENCES objective_revisions (objective_id, revision)
        DEFERRABLE INITIALLY DEFERRED;

-- Current BlueprintRevision per Blueprint.
CREATE TABLE blueprints (
    blueprint_id     uuid    PRIMARY KEY CHECK (uuid_extract_version(blueprint_id) = 7),
    project_id       uuid    NOT NULL REFERENCES projects,
    current_revision integer NOT NULL CHECK (current_revision > 0),
    UNIQUE (project_id, blueprint_id)
);

-- Immutable BlueprintRevision snapshots. Only status changes after insert,
-- and only from proposed to active or rejected. The snapshot document never
-- contains status.
CREATE TABLE blueprint_revisions (
    blueprint_id         uuid        NOT NULL,
    revision             integer     NOT NULL CHECK (revision > 0),
    previous_revision    integer     GENERATED ALWAYS AS (NULLIF(revision - 1, 0)) STORED,
    project_id           uuid        NOT NULL,
    objective_id         uuid        NOT NULL,
    objective_revision   integer     NOT NULL,
    proposed_by_human_id uuid        NOT NULL,
    status               text        NOT NULL CHECK (status IN ('proposed', 'active', 'rejected')),
    created_at           timestamptz NOT NULL,
    document             jsonb       NOT NULL,
    PRIMARY KEY (blueprint_id, revision),
    FOREIGN KEY (blueprint_id, previous_revision) REFERENCES blueprint_revisions (blueprint_id, revision),
    FOREIGN KEY (project_id, blueprint_id) REFERENCES blueprints (project_id, blueprint_id),
    FOREIGN KEY (project_id, objective_id) REFERENCES objectives (project_id, objective_id),
    FOREIGN KEY (objective_id, objective_revision) REFERENCES objective_revisions (objective_id, revision),
    FOREIGN KEY (project_id, proposed_by_human_id) REFERENCES project_memberships (project_id, human_id),
    CHECK (jsonb_typeof(document) = 'object'),
    CHECK (NOT document ? 'status'),
    CHECK (jsonb_typeof(document -> 'cell_charters') = 'array'),
    CHECK (document -> 'blueprint_id' = to_jsonb(blueprint_id::text)),
    CHECK (document -> 'project_id' = to_jsonb(project_id::text)),
    CHECK (document -> 'revision' = to_jsonb(revision)),
    CHECK (document -> 'objective_id' = to_jsonb(objective_id::text)),
    CHECK (document -> 'objective_revision' = to_jsonb(objective_revision)),
    CHECK (document -> 'proposed_by_human_id' = to_jsonb(proposed_by_human_id::text))
);

ALTER TABLE blueprints
    ADD FOREIGN KEY (blueprint_id, current_revision)
        REFERENCES blueprint_revisions (blueprint_id, revision)
        DEFERRABLE INITIALLY DEFERRED;

-- +goose StatementBegin
CREATE FUNCTION conn_guard_blueprint_revision() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.blueprint_id, NEW.revision, NEW.project_id, NEW.objective_id, NEW.objective_revision,
        NEW.proposed_by_human_id, NEW.created_at, NEW.document)
       IS DISTINCT FROM
       (OLD.blueprint_id, OLD.revision, OLD.project_id, OLD.objective_id, OLD.objective_revision,
        OLD.proposed_by_human_id, OLD.created_at, OLD.document) THEN
        RAISE EXCEPTION 'blueprint revision snapshots are immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    IF NEW.status IS DISTINCT FROM OLD.status
       AND NOT (OLD.status = 'proposed' AND NEW.status IN ('active', 'rejected')) THEN
        RAISE EXCEPTION 'blueprint revision status cannot change from % to %', OLD.status, NEW.status
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- One row per CellCharter in a BlueprintRevision, for queries and authority
-- checks. Reconstructible from blueprint_revisions.document.
CREATE TABLE blueprint_cells (
    blueprint_id         uuid    NOT NULL,
    blueprint_revision   integer NOT NULL,
    cell_id              uuid    NOT NULL CHECK (uuid_extract_version(cell_id) = 7),
    project_id           uuid    NOT NULL,
    accountable_human_id uuid    NOT NULL,
    PRIMARY KEY (blueprint_id, blueprint_revision, cell_id),
    UNIQUE (blueprint_id, blueprint_revision, cell_id, accountable_human_id),
    FOREIGN KEY (blueprint_id, blueprint_revision) REFERENCES blueprint_revisions (blueprint_id, revision),
    FOREIGN KEY (project_id, blueprint_id) REFERENCES blueprints (project_id, blueprint_id),
    FOREIGN KEY (project_id, accountable_human_id) REFERENCES project_memberships (project_id, human_id)
);
CREATE INDEX blueprint_cells_accountable_human_id_idx ON blueprint_cells (accountable_human_id);

-- Append-only acceptance or rejection, at most one per cell per revision.
CREATE TABLE charter_decisions (
    decision_id        uuid        PRIMARY KEY CHECK (uuid_extract_version(decision_id) = 7),
    project_id         uuid        NOT NULL,
    blueprint_id       uuid        NOT NULL,
    blueprint_revision integer     NOT NULL,
    cell_id            uuid        NOT NULL,
    human_id           uuid        NOT NULL,
    outcome            text        NOT NULL CHECK (outcome IN ('accept', 'reject')),
    rationale          text        CHECK (rationale IS NULL OR char_length(rationale) BETWEEN 1 AND 4000),
    decided_at         timestamptz NOT NULL,
    UNIQUE (blueprint_id, blueprint_revision, cell_id),
    FOREIGN KEY (project_id, blueprint_id) REFERENCES blueprints (project_id, blueprint_id),
    -- Only the cell's accountable human can decide on its charter.
    FOREIGN KEY (blueprint_id, blueprint_revision, cell_id, human_id)
        REFERENCES blueprint_cells (blueprint_id, blueprint_revision, cell_id, accountable_human_id),
    CHECK (outcome = 'accept' OR rationale IS NOT NULL)
);

-- Locked and advanced in the same transaction as each state change, so
-- committed event sequences are gap-free within a project.
CREATE TABLE project_event_counters (
    project_id          uuid   PRIMARY KEY REFERENCES projects,
    last_event_sequence bigint NOT NULL DEFAULT 0
        CHECK (last_event_sequence BETWEEN 0 AND 9007199254740991)
);

CREATE TABLE events (
    project_id        uuid        NOT NULL REFERENCES projects,
    event_sequence    bigint      NOT NULL CHECK (event_sequence BETWEEN 1 AND 9007199254740991),
    -- Sequence n requires sequence n - 1 in the same project.
    previous_sequence bigint      GENERATED ALWAYS AS (NULLIF(event_sequence - 1, 0)) STORED,
    event_id          uuid        NOT NULL UNIQUE CHECK (uuid_extract_version(event_id) = 7),
    event_type        text        NOT NULL,
    entity_type       text        NOT NULL,
    entity_id         uuid        NOT NULL,
    actor_human_id    uuid        NOT NULL REFERENCES humans,
    occurred_at       timestamptz NOT NULL,
    payload           jsonb       NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    PRIMARY KEY (project_id, event_sequence),
    FOREIGN KEY (project_id, previous_sequence) REFERENCES events (project_id, event_sequence),
    CHECK ((event_type, entity_type) IN (
        ('project.created', 'project'),
        ('project.member_added', 'project'),
        ('objective.revision_created', 'objective'),
        ('blueprint.revision_proposed', 'blueprint'),
        ('blueprint.revision_activated', 'blueprint'),
        ('blueprint.revision_rejected', 'blueprint'),
        ('charter.accepted', 'charter_decision'),
        ('charter.rejected', 'charter_decision')
    ))
);

CREATE TRIGGER project_memberships_append_only BEFORE UPDATE OR DELETE ON project_memberships
    FOR EACH ROW EXECUTE FUNCTION conn_reject_mutation();
CREATE TRIGGER objective_revisions_append_only BEFORE UPDATE OR DELETE ON objective_revisions
    FOR EACH ROW EXECUTE FUNCTION conn_reject_mutation();
CREATE TRIGGER blueprint_revisions_guard BEFORE UPDATE ON blueprint_revisions
    FOR EACH ROW EXECUTE FUNCTION conn_guard_blueprint_revision();
CREATE TRIGGER blueprint_revisions_no_delete BEFORE DELETE ON blueprint_revisions
    FOR EACH ROW EXECUTE FUNCTION conn_reject_mutation();
CREATE TRIGGER blueprint_cells_append_only BEFORE UPDATE OR DELETE ON blueprint_cells
    FOR EACH ROW EXECUTE FUNCTION conn_reject_mutation();
CREATE TRIGGER charter_decisions_append_only BEFORE UPDATE OR DELETE ON charter_decisions
    FOR EACH ROW EXECUTE FUNCTION conn_reject_mutation();
CREATE TRIGGER events_append_only BEFORE UPDATE OR DELETE ON events
    FOR EACH ROW EXECUTE FUNCTION conn_reject_mutation();

-- +goose Down
-- Development only. Automation never runs down migrations.

DROP TABLE events;
DROP TABLE project_event_counters;
DROP TABLE charter_decisions;
DROP TABLE blueprint_cells;
ALTER TABLE blueprints DROP CONSTRAINT blueprints_blueprint_id_current_revision_fkey;
DROP TABLE blueprint_revisions;
DROP FUNCTION conn_guard_blueprint_revision();
DROP TABLE blueprints;
ALTER TABLE objectives DROP CONSTRAINT objectives_objective_id_current_revision_fkey;
DROP TABLE objective_revisions;
DROP TABLE objectives;
ALTER TABLE projects DROP CONSTRAINT projects_project_id_owner_human_id_owner_role_fkey;
DROP TABLE project_memberships;
DROP TABLE projects;
DROP TABLE sessions;
DROP TABLE humans;
DROP FUNCTION conn_reject_mutation();
