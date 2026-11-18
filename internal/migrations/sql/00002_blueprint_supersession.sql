-- Blueprint revision lifecycle and proposal authority.
--
-- A blueprint has at most one active revision, which stays in force while a
-- replacement is reviewed, and at most one pending proposal. Activating a
-- revision supersedes the previous active one, and proposing a new revision
-- supersedes a pending one, in the same transaction. Only project owners
-- propose.

-- +goose Up

ALTER TABLE blueprint_revisions DROP CONSTRAINT blueprint_revisions_status_check;
ALTER TABLE blueprint_revisions
    ADD CONSTRAINT blueprint_revisions_status_check
        CHECK (status IN ('proposed', 'active', 'rejected', 'superseded'));

CREATE UNIQUE INDEX blueprint_revisions_one_active_idx
    ON blueprint_revisions (blueprint_id) WHERE status = 'active';
CREATE UNIQUE INDEX blueprint_revisions_one_proposed_idx
    ON blueprint_revisions (blueprint_id) WHERE status = 'proposed';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION conn_guard_blueprint_revision() RETURNS trigger
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
       AND NOT (OLD.status = 'proposed' AND NEW.status IN ('active', 'rejected', 'superseded'))
       AND NOT (OLD.status = 'active' AND NEW.status = 'superseded') THEN
        RAISE EXCEPTION 'blueprint revision status cannot change from % to %', OLD.status, NEW.status
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- The proposer must hold the owner membership.
ALTER TABLE blueprint_revisions
    ADD COLUMN proposer_role text NOT NULL GENERATED ALWAYS AS ('owner') STORED;
ALTER TABLE blueprint_revisions
    ADD CONSTRAINT blueprint_revisions_proposer_owner_fkey
        FOREIGN KEY (project_id, proposed_by_human_id, proposer_role)
        REFERENCES project_memberships (project_id, human_id, role);

-- +goose Down
-- Development only. Automation never runs down migrations.

ALTER TABLE blueprint_revisions DROP CONSTRAINT blueprint_revisions_proposer_owner_fkey;
ALTER TABLE blueprint_revisions DROP COLUMN proposer_role;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION conn_guard_blueprint_revision() RETURNS trigger
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

DROP INDEX blueprint_revisions_one_proposed_idx;
DROP INDEX blueprint_revisions_one_active_idx;
ALTER TABLE blueprint_revisions DROP CONSTRAINT blueprint_revisions_status_check;
ALTER TABLE blueprint_revisions
    ADD CONSTRAINT blueprint_revisions_status_check
        CHECK (status IN ('proposed', 'active', 'rejected'));
