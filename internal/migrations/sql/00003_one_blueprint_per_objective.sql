-- One blueprint per Objective (decision 0005).
--
-- An Objective has at most one blueprint, and every revision of a blueprint
-- divides that same Objective. A changed plan is the blueprint's next
-- revision, so at most one partition of an Objective is ever active.

-- +goose Up

ALTER TABLE blueprints ADD COLUMN objective_id uuid;

UPDATE blueprints b
SET objective_id = r.objective_id
FROM blueprint_revisions r
WHERE r.blueprint_id = b.blueprint_id AND r.revision = 1;

-- Existing data must already follow the rule. Revision history is
-- append-only, so a violation needs a deliberate manual decision rather
-- than an automatic repair.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM blueprints GROUP BY objective_id HAVING count(*) > 1) THEN
        RAISE EXCEPTION 'an Objective has more than one blueprint; resolve it before applying migration 3'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    IF EXISTS (
        SELECT 1 FROM blueprint_revisions r
        JOIN blueprints b USING (blueprint_id)
        WHERE r.objective_id IS DISTINCT FROM b.objective_id
    ) THEN
        RAISE EXCEPTION 'a blueprint has revisions for different Objectives; resolve it before applying migration 3'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE blueprints ALTER COLUMN objective_id SET NOT NULL;
ALTER TABLE blueprints
    ADD CONSTRAINT blueprints_objective_id_key UNIQUE (objective_id);
ALTER TABLE blueprints
    ADD CONSTRAINT blueprints_blueprint_id_objective_id_key UNIQUE (blueprint_id, objective_id);
ALTER TABLE blueprints
    ADD CONSTRAINT blueprints_project_id_objective_id_fkey
        FOREIGN KEY (project_id, objective_id) REFERENCES objectives (project_id, objective_id);

-- Every revision divides its blueprint's Objective.
ALTER TABLE blueprint_revisions
    ADD CONSTRAINT blueprint_revisions_blueprint_objective_fkey
        FOREIGN KEY (blueprint_id, objective_id) REFERENCES blueprints (blueprint_id, objective_id);

-- +goose Down
-- Development only. Automation never runs down migrations.

ALTER TABLE blueprint_revisions DROP CONSTRAINT blueprint_revisions_blueprint_objective_fkey;
ALTER TABLE blueprints DROP CONSTRAINT blueprints_project_id_objective_id_fkey;
ALTER TABLE blueprints DROP CONSTRAINT blueprints_blueprint_id_objective_id_key;
ALTER TABLE blueprints DROP CONSTRAINT blueprints_objective_id_key;
ALTER TABLE blueprints DROP COLUMN objective_id;
