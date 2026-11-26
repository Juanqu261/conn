-- Collision lifecycle driven by human decisions.
--
-- Each assessment stores the normalized subjects that matched its rule and an
-- optional reference to the human decision that set, or was inherited by, its
-- state. A later Intent revision inherits a dismissed or resolved state only
-- when its matched subjects are a subset of the decided subjects, so the
-- lookup index serves the most recent assessment for an Intent pair and rule.

-- +goose Up

ALTER TABLE collision_assessments
    ADD COLUMN matched_subjects text[] NOT NULL DEFAULT '{}',
    ADD COLUMN decision_id uuid NULL;

ALTER TABLE collision_assessments
    ADD CONSTRAINT collision_assessments_decision_fkey
    FOREIGN KEY (decision_id, project_id)
    REFERENCES resolution_decisions (decision_id, project_id);

ALTER TABLE collision_assessments
    ADD CONSTRAINT collision_assessments_matched_subjects_bound
    CHECK (cardinality(matched_subjects) <= 50);

-- Existing assessments derive their matched subjects from their immutable
-- matching claims. Every assessment references at least two claims, so the
-- backfilled list is never empty. The document mirrors the column because
-- reads decode the canonical document.
UPDATE collision_assessments a
SET matched_subjects = s.subjects,
    document = jsonb_set(a.document, '{matched_subjects}', to_jsonb(s.subjects))
FROM (
    SELECT m.collision_id,
           (array_agg(DISTINCT c.normalized_subject ORDER BY c.normalized_subject))[1:50] AS subjects
    FROM collision_claim_matches m
    JOIN semantic_claims c ON c.claim_id = m.claim_id AND c.project_id = m.project_id
    GROUP BY m.collision_id
) s
WHERE a.collision_id = s.collision_id;

CREATE INDEX collision_assessments_carry_forward_idx
    ON collision_assessments (project_id, left_intent_id, right_intent_id, rule_id, created_at DESC);

-- +goose Down
-- Development only. Automation never runs down migrations.

DROP INDEX collision_assessments_carry_forward_idx;
UPDATE collision_assessments SET document = document - 'matched_subjects' - 'decision_ref';
ALTER TABLE collision_assessments DROP CONSTRAINT collision_assessments_matched_subjects_bound;
ALTER TABLE collision_assessments DROP CONSTRAINT collision_assessments_decision_fkey;
ALTER TABLE collision_assessments DROP COLUMN decision_id, DROP COLUMN matched_subjects;
