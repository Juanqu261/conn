-- Combination checks from `conn verify-pair`.
--
-- A combination check is an immutable, advisory record of testing the two
-- branches of one collision together on a participant's machine. It never
-- changes the collision state. The document is the canonical record; the
-- columns serve lookups and the insights outcome counts.

-- +goose Up

CREATE TABLE combination_checks (
    check_id                uuid        PRIMARY KEY CHECK (uuid_extract_version(check_id) = 7),
    project_id              uuid        NOT NULL,
    collision_id            uuid        NOT NULL,
    outcome                 text        NOT NULL CHECK (outcome IN ('passing', 'failing', 'merge_conflict', 'incomplete')),
    producer_participant_id uuid        NOT NULL REFERENCES participants,
    created_at              timestamptz NOT NULL,
    document                jsonb       NOT NULL,
    FOREIGN KEY (collision_id, project_id)
        REFERENCES collision_assessments (collision_id, project_id),
    CHECK (jsonb_typeof(document) = 'object'),
    CHECK (document -> 'check_id' = to_jsonb(check_id::text)),
    CHECK (document -> 'project_id' = to_jsonb(project_id::text)),
    CHECK (document -> 'collision_id' = to_jsonb(collision_id::text)),
    CHECK (document -> 'outcome' = to_jsonb(outcome))
);
CREATE INDEX combination_checks_collision_idx
    ON combination_checks (project_id, collision_id, created_at DESC, check_id DESC);
CREATE TRIGGER combination_checks_append_only BEFORE UPDATE OR DELETE ON combination_checks
    FOR EACH ROW EXECUTE FUNCTION conn_reject_mutation();

ALTER TABLE events DROP CONSTRAINT events_check;
ALTER TABLE events ADD CONSTRAINT events_check CHECK ((event_type, entity_type) IN (
    ('project.created', 'project'),
    ('project.member_added', 'project'),
    ('objective.revision_created', 'objective'),
    ('blueprint.revision_proposed', 'blueprint'),
    ('blueprint.revision_activated', 'blueprint'),
    ('blueprint.revision_rejected', 'blueprint'),
    ('charter.accepted', 'charter_decision'),
    ('charter.rejected', 'charter_decision'),
    ('repository.connected', 'repository'),
    ('intent.published', 'intent'),
    ('intent.revised', 'intent'),
    ('intent.released', 'intent'),
    ('claims.observed', 'semantic_claim'),
    ('collision.opened', 'collision'),
    ('collision.updated', 'collision'),
    ('resolution.proposed', 'resolution_proposal'),
    ('resolution.decided', 'resolution_decision'),
    ('evidence.attached', 'evidence_reference'),
    ('objective.imported', 'objective'),
    ('combination.checked', 'combination_check')
));

-- +goose Down
-- Development only. Automation never runs down migrations.

ALTER TABLE events DROP CONSTRAINT events_check;
ALTER TABLE events ADD CONSTRAINT events_check CHECK ((event_type, entity_type) IN (
    ('project.created', 'project'),
    ('project.member_added', 'project'),
    ('objective.revision_created', 'objective'),
    ('blueprint.revision_proposed', 'blueprint'),
    ('blueprint.revision_activated', 'blueprint'),
    ('blueprint.revision_rejected', 'blueprint'),
    ('charter.accepted', 'charter_decision'),
    ('charter.rejected', 'charter_decision'),
    ('repository.connected', 'repository'),
    ('intent.published', 'intent'),
    ('intent.revised', 'intent'),
    ('intent.released', 'intent'),
    ('claims.observed', 'semantic_claim'),
    ('collision.opened', 'collision'),
    ('collision.updated', 'collision'),
    ('resolution.proposed', 'resolution_proposal'),
    ('resolution.decided', 'resolution_decision'),
    ('evidence.attached', 'evidence_reference'),
    ('objective.imported', 'objective')
));
DROP TRIGGER combination_checks_append_only ON combination_checks;
DROP TABLE combination_checks;