-- Observed check runs as evidence.
--
-- Evidence may carry an observation record of a command the Conn CLI ran.
-- The record lives in the canonical document; the column mirrors it so
-- check_run evidence and its observation stay consistent. The index serves
-- the newest-first evidence list of one Intent.

-- +goose Up

ALTER TABLE evidence_references DROP CONSTRAINT evidence_references_kind_check;
ALTER TABLE evidence_references ADD CONSTRAINT evidence_references_kind_check CHECK (kind IN (
    'commit', 'pull_request', 'ci_run', 'test_case', 'trace', 'artifact', 'decision', 'external_link', 'check_run'
));

ALTER TABLE evidence_references ADD COLUMN observation jsonb NULL;

ALTER TABLE evidence_references ADD CONSTRAINT evidence_references_observation_check CHECK (
    (kind = 'check_run') = (observation IS NOT NULL)
    AND (observation IS NULL OR (
        jsonb_typeof(observation) = 'object'
        AND document -> 'observation' = observation
    ))
);

CREATE INDEX evidence_references_intent_idx
    ON evidence_references (project_id, intent_id, observed_at DESC);

-- +goose Down
-- Development only. Automation never runs down migrations.

DROP INDEX evidence_references_intent_idx;
ALTER TABLE evidence_references DROP CONSTRAINT evidence_references_observation_check;
ALTER TABLE evidence_references DROP COLUMN observation;
ALTER TABLE evidence_references DROP CONSTRAINT evidence_references_kind_check;
ALTER TABLE evidence_references ADD CONSTRAINT evidence_references_kind_check CHECK (kind IN (
    'commit', 'pull_request', 'ci_run', 'test_case', 'trace', 'artifact', 'decision', 'external_link'
));
