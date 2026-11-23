-- Deterministic collision assessor identity and lifecycle consistency.
--
-- One advisory assessment aggregates every matching claim pair for a rule and
-- an exact pair of active Intent revisions. This unique identity makes retries
-- idempotent while retaining all matching claim IDs in collision_claim_matches.

-- +goose Up

ALTER TABLE collision_assessments
    ADD CONSTRAINT collision_assessments_rule_identity_key
    UNIQUE (project_id, left_intent_id, left_intent_revision,
            right_intent_id, right_intent_revision, rule_id);

-- +goose Down
-- Development only. Automation never runs down migrations.

ALTER TABLE collision_assessments
    DROP CONSTRAINT collision_assessments_rule_identity_key;
