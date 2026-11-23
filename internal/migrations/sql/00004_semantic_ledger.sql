-- Semantic coordination ledger
--
-- Conn records repository connections, human-bound operating participants,
-- immutable Intent revisions and claim observations, advisory collision
-- assessments, resolution history, and reference-first evidence. The schema
-- retains source facts and project-scoped foreign keys so future Radar reads
-- can explain why a relationship existed at a particular event cursor.

-- +goose Up

CREATE TABLE repository_connections (
    repository_id           uuid        PRIMARY KEY CHECK (uuid_extract_version(repository_id) = 7),
    project_id              uuid        NOT NULL REFERENCES projects,
    host_kind               text        NOT NULL CHECK (host_kind IN ('github', 'generic_git')),
    canonical_remote        text        NOT NULL
        CHECK (canonical_remote ~ '^[a-z0-9][a-z0-9.-]*(/[A-Za-z0-9._~-]+)+$'),
    host_repository_node_id text,
    default_branch          text,
    analysis_enabled        boolean     NOT NULL,
    created_by_human_id     uuid        NOT NULL,
    created_at              timestamptz NOT NULL,
    UNIQUE (project_id, repository_id),
    UNIQUE (project_id, canonical_remote),
    FOREIGN KEY (project_id, created_by_human_id)
        REFERENCES project_memberships (project_id, human_id)
);
CREATE UNIQUE INDEX repository_connections_host_node_idx
    ON repository_connections (host_kind, host_repository_node_id)
    WHERE host_repository_node_id IS NOT NULL;

CREATE TABLE participants (
    participant_id    uuid        PRIMARY KEY CHECK (uuid_extract_version(participant_id) = 7),
    project_id        uuid        NOT NULL REFERENCES projects,
    human_id          uuid        NOT NULL,
    adapter_kind      text        NOT NULL CHECK (adapter_kind IN ('kiro', 'cli', 'ci', 'github', 'other_registered_adapter')),
    local_session_ref text,
    display_label     text,
    created_at        timestamptz NOT NULL,
    retired_at        timestamptz,
    UNIQUE (project_id, participant_id),
    UNIQUE (project_id, human_id, adapter_kind, local_session_ref),
    FOREIGN KEY (project_id, human_id) REFERENCES project_memberships (project_id, human_id),
    CHECK (retired_at IS NULL OR retired_at >= created_at)
);
CREATE INDEX participants_project_human_idx ON participants (project_id, human_id);

CREATE TABLE intents (
    intent_id        uuid        PRIMARY KEY CHECK (uuid_extract_version(intent_id) = 7),
    project_id       uuid        NOT NULL REFERENCES projects,
    participant_id   uuid        NOT NULL,
    status           text        NOT NULL CHECK (status IN ('active', 'released', 'superseded')),
    current_revision integer     NOT NULL CHECK (current_revision > 0),
    created_at       timestamptz NOT NULL,
    UNIQUE (project_id, intent_id),
    UNIQUE (intent_id, project_id, participant_id),
    FOREIGN KEY (project_id, participant_id) REFERENCES participants (project_id, participant_id)
);
CREATE INDEX intents_project_active_idx ON intents (project_id, created_at, intent_id)
    WHERE status = 'active';

CREATE TABLE intent_revisions (
    intent_id          uuid        NOT NULL,
    revision           integer     NOT NULL CHECK (revision > 0),
    previous_revision  integer     GENERATED ALWAYS AS (NULLIF(revision - 1, 0)) STORED,
    project_id         uuid        NOT NULL,
    participant_id     uuid        NOT NULL,
    objective_id       uuid        NOT NULL,
    objective_revision integer     NOT NULL,
    blueprint_id       uuid,
    blueprint_revision integer,
    charter_id         uuid,
    repository_id      uuid,
    base_revision_ref  text,
    head_revision_ref  text,
    branch_ref         text,
    created_at         timestamptz NOT NULL,
    document           jsonb       NOT NULL,
    PRIMARY KEY (intent_id, revision),
    UNIQUE (project_id, intent_id, revision),
    FOREIGN KEY (intent_id, previous_revision) REFERENCES intent_revisions (intent_id, revision),
    FOREIGN KEY (intent_id, project_id, participant_id)
        REFERENCES intents (intent_id, project_id, participant_id),
    FOREIGN KEY (project_id, objective_id) REFERENCES objectives (project_id, objective_id),
    FOREIGN KEY (objective_id, objective_revision) REFERENCES objective_revisions (objective_id, revision),
    FOREIGN KEY (project_id, repository_id) REFERENCES repository_connections (project_id, repository_id),
    FOREIGN KEY (blueprint_id, blueprint_revision) REFERENCES blueprint_revisions (blueprint_id, revision),
    FOREIGN KEY (blueprint_id, blueprint_revision, charter_id)
        REFERENCES blueprint_cells (blueprint_id, blueprint_revision, cell_id),
    CHECK ((blueprint_id IS NULL) = (blueprint_revision IS NULL)),
    CHECK (charter_id IS NULL OR blueprint_id IS NOT NULL),
    CHECK (jsonb_typeof(document) = 'object'),
    CHECK (document -> 'intent_id' = to_jsonb(intent_id::text)),
    CHECK (document -> 'project_id' = to_jsonb(project_id::text)),
    CHECK (document -> 'participant_id' = to_jsonb(participant_id::text)),
    CHECK (document -> 'revision' = to_jsonb(revision)),
    CHECK (document -> 'objective_id' = to_jsonb(objective_id::text)),
    CHECK (document -> 'objective_revision' = to_jsonb(objective_revision)),
    CHECK (document -> 'created_at' IS NOT NULL)
);
ALTER TABLE intents
    ADD CONSTRAINT intents_current_revision_fkey
    FOREIGN KEY (intent_id, current_revision)
    REFERENCES intent_revisions (intent_id, revision)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE semantic_claims (
    claim_id                uuid        PRIMARY KEY CHECK (uuid_extract_version(claim_id) = 7),
    project_id              uuid        NOT NULL,
    intent_id               uuid        NOT NULL,
    intent_revision         integer     NOT NULL,
    subject_kind            text        NOT NULL CHECK (subject_kind IN (
        'behavior', 'invariant', 'contract', 'data_resource', 'infrastructure_resource',
        'code_symbol', 'dependency', 'assumption', 'question', 'acceptance_criterion'
    )),
    normalized_subject      text        NOT NULL CHECK (char_length(normalized_subject) BETWEEN 3 AND 512),
    display_name            text        NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 200),
    relation                text        NOT NULL CHECK (relation IN (
        'affects', 'changes', 'consumes', 'produces', 'depends_on', 'removes', 'verifies', 'assumes', 'questions'
    )),
    provenance_kind         text        NOT NULL CHECK (provenance_kind IN (
        'declared', 'static_analysis', 'external_import', 'ci_evidence', 'model_inference'
    )),
    confidence              double precision,
    producer_participant_id uuid,
    observed_at             timestamptz NOT NULL,
    document                jsonb       NOT NULL,
    UNIQUE (claim_id, project_id),
    FOREIGN KEY (project_id, intent_id, intent_revision)
        REFERENCES intent_revisions (project_id, intent_id, revision),
    FOREIGN KEY (project_id, producer_participant_id)
        REFERENCES participants (project_id, participant_id),
    CHECK (confidence IS NULL OR confidence BETWEEN 0 AND 1),
    CHECK (provenance_kind <> 'model_inference' OR confidence IS NOT NULL),
    CHECK (jsonb_typeof(document) = 'object'),
    CHECK (document -> 'claim_id' = to_jsonb(claim_id::text)),
    CHECK (document -> 'project_id' = to_jsonb(project_id::text)),
    CHECK (document -> 'intent_id' = to_jsonb(intent_id::text)),
    CHECK (document -> 'intent_revision' = to_jsonb(intent_revision)),
    CHECK (document -> 'observed_at' IS NOT NULL)
);
CREATE INDEX semantic_claims_project_subject_idx
    ON semantic_claims (project_id, normalized_subject, observed_at, claim_id);
CREATE INDEX semantic_claims_intent_revision_idx
    ON semantic_claims (project_id, intent_id, intent_revision);

CREATE TABLE collision_assessments (
    collision_id          uuid        PRIMARY KEY CHECK (uuid_extract_version(collision_id) = 7),
    project_id            uuid        NOT NULL REFERENCES projects,
    left_intent_id        uuid        NOT NULL,
    left_intent_revision  integer     NOT NULL,
    right_intent_id       uuid        NOT NULL,
    right_intent_revision integer     NOT NULL,
    rule_id               text        NOT NULL CHECK (rule_id IN (
        'shared_declared_invariant', 'shared_exported_symbol',
        'contract_producer_consumer', 'schema_resource_overlap'
    )),
    severity              text        NOT NULL CHECK (severity IN ('info', 'amber', 'red')),
    confidence            double precision NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    state                 text        NOT NULL CHECK (state IN ('open', 'under_review', 'resolved', 'dismissed', 'stale')),
    explanation_markdown  text        NOT NULL CHECK (char_length(explanation_markdown) BETWEEN 1 AND 32768),
    created_at            timestamptz NOT NULL,
    superseded_at         timestamptz,
    document              jsonb       NOT NULL,
    UNIQUE (collision_id, project_id),
    FOREIGN KEY (project_id, left_intent_id, left_intent_revision)
        REFERENCES intent_revisions (project_id, intent_id, revision),
    FOREIGN KEY (project_id, right_intent_id, right_intent_revision)
        REFERENCES intent_revisions (project_id, intent_id, revision),
    CHECK (left_intent_id <> right_intent_id OR left_intent_revision <> right_intent_revision),
    CHECK (superseded_at IS NULL OR superseded_at >= created_at),
    CHECK (jsonb_typeof(document) = 'object'),
    CHECK (document -> 'collision_id' = to_jsonb(collision_id::text)),
    CHECK (document -> 'project_id' = to_jsonb(project_id::text)),
    CHECK (document -> 'created_at' IS NOT NULL)
);
CREATE INDEX collision_assessments_project_open_idx
    ON collision_assessments (project_id, severity, created_at, collision_id)
    WHERE state IN ('open', 'under_review');

CREATE TABLE collision_claim_matches (
    collision_id uuid NOT NULL,
    claim_id     uuid NOT NULL,
    project_id   uuid NOT NULL,
    PRIMARY KEY (collision_id, claim_id),
    FOREIGN KEY (collision_id, project_id)
        REFERENCES collision_assessments (collision_id, project_id),
    FOREIGN KEY (claim_id, project_id) REFERENCES semantic_claims (claim_id, project_id)
);

CREATE TABLE resolution_proposals (
    proposal_id                 uuid        PRIMARY KEY CHECK (uuid_extract_version(proposal_id) = 7),
    project_id                  uuid        NOT NULL,
    collision_id                uuid        NOT NULL,
    proposed_by_participant_id  uuid,
    proposed_by_human_id        uuid,
    kind                        text        NOT NULL CHECK (kind IN (
        'dependency', 'sequencing', 'contract_agreement', 'accepted_risk',
        'false_positive', 'revise_objective', 'revise_blueprint', 'other'
    )),
    rationale_markdown          text        NOT NULL CHECK (char_length(rationale_markdown) BETWEEN 1 AND 4000),
    references_json             jsonb       NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(references_json) = 'array'),
    created_at                  timestamptz NOT NULL,
    UNIQUE (proposal_id, project_id),
    FOREIGN KEY (collision_id, project_id)
        REFERENCES collision_assessments (collision_id, project_id),
    FOREIGN KEY (project_id, proposed_by_participant_id)
        REFERENCES participants (project_id, participant_id),
    FOREIGN KEY (project_id, proposed_by_human_id)
        REFERENCES project_memberships (project_id, human_id),
    CHECK (num_nonnulls(proposed_by_participant_id, proposed_by_human_id) = 1)
);

CREATE TABLE resolution_decisions (
    decision_id           uuid        PRIMARY KEY CHECK (uuid_extract_version(decision_id) = 7),
    project_id            uuid        NOT NULL,
    collision_id          uuid        NOT NULL,
    selected_proposal_id  uuid,
    decided_by_human_id   uuid        NOT NULL,
    kind                  text        NOT NULL CHECK (kind IN (
        'dependency', 'sequencing', 'contract_agreement', 'accepted_risk',
        'false_positive', 'revise_objective', 'revise_blueprint', 'other'
    )),
    binding               boolean     NOT NULL,
    rationale_markdown    text        NOT NULL CHECK (char_length(rationale_markdown) BETWEEN 1 AND 4000),
    references_json       jsonb       NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(references_json) = 'array'),
    created_at            timestamptz NOT NULL,
    UNIQUE (decision_id, project_id),
    FOREIGN KEY (collision_id, project_id)
        REFERENCES collision_assessments (collision_id, project_id),
    FOREIGN KEY (selected_proposal_id, project_id)
        REFERENCES resolution_proposals (proposal_id, project_id),
    FOREIGN KEY (project_id, decided_by_human_id)
        REFERENCES project_memberships (project_id, human_id),
    CHECK (kind NOT IN ('contract_agreement', 'accepted_risk', 'revise_objective', 'revise_blueprint') OR binding)
);
CREATE INDEX resolution_decisions_collision_idx ON resolution_decisions (project_id, collision_id, created_at);

CREATE TABLE evidence_references (
    evidence_id              uuid        PRIMARY KEY CHECK (uuid_extract_version(evidence_id) = 7),
    project_id               uuid        NOT NULL,
    intent_id                uuid,
    objective_id             uuid        NOT NULL,
    objective_revision       integer     NOT NULL,
    acceptance_key           text,
    kind                     text        NOT NULL CHECK (kind IN (
        'commit', 'pull_request', 'ci_run', 'test_case', 'trace', 'artifact', 'decision', 'external_link'
    )),
    state                    text        NOT NULL CHECK (state IN ('passing', 'failing', 'missing', 'stale', 'inaccessible', 'unknown')),
    locator                  text        NOT NULL CHECK (char_length(locator) BETWEEN 1 AND 256),
    content_hash             text        CHECK (content_hash IS NULL OR content_hash ~ '^[0-9a-f]{64}$'),
    summary                  text,
    producer_participant_id  uuid,
    observed_at              timestamptz NOT NULL,
    document                 jsonb       NOT NULL,
    UNIQUE (evidence_id, project_id),
    FOREIGN KEY (project_id, intent_id) REFERENCES intents (project_id, intent_id),
    FOREIGN KEY (project_id, objective_id) REFERENCES objectives (project_id, objective_id),
    FOREIGN KEY (objective_id, objective_revision) REFERENCES objective_revisions (objective_id, revision),
    FOREIGN KEY (project_id, producer_participant_id)
        REFERENCES participants (project_id, participant_id),
    CHECK (jsonb_typeof(document) = 'object'),
    CHECK (document -> 'evidence_id' = to_jsonb(evidence_id::text)),
    CHECK (document -> 'project_id' = to_jsonb(project_id::text)),
    CHECK (document -> 'objective_id' = to_jsonb(objective_id::text)),
    CHECK (document -> 'objective_revision' = to_jsonb(objective_revision)),
    CHECK (document -> 'observed_at' IS NOT NULL)
);
CREATE INDEX evidence_references_objective_idx
    ON evidence_references (project_id, objective_id, objective_revision, acceptance_key, observed_at);

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

CREATE TRIGGER repository_connections_append_only BEFORE UPDATE OR DELETE ON repository_connections
    FOR EACH ROW EXECUTE FUNCTION conn_reject_mutation();
CREATE TRIGGER intent_revisions_append_only BEFORE UPDATE OR DELETE ON intent_revisions
    FOR EACH ROW EXECUTE FUNCTION conn_reject_mutation();
CREATE TRIGGER semantic_claims_append_only BEFORE UPDATE OR DELETE ON semantic_claims
    FOR EACH ROW EXECUTE FUNCTION conn_reject_mutation();
CREATE TRIGGER collision_claim_matches_append_only BEFORE UPDATE OR DELETE ON collision_claim_matches
    FOR EACH ROW EXECUTE FUNCTION conn_reject_mutation();
CREATE TRIGGER resolution_proposals_append_only BEFORE UPDATE OR DELETE ON resolution_proposals
    FOR EACH ROW EXECUTE FUNCTION conn_reject_mutation();
CREATE TRIGGER resolution_decisions_append_only BEFORE UPDATE OR DELETE ON resolution_decisions
    FOR EACH ROW EXECUTE FUNCTION conn_reject_mutation();
CREATE TRIGGER evidence_references_append_only BEFORE UPDATE OR DELETE ON evidence_references
    FOR EACH ROW EXECUTE FUNCTION conn_reject_mutation();

-- +goose Down
-- Development only. Automation never runs down migrations.

DROP TRIGGER evidence_references_append_only ON evidence_references;
DROP TRIGGER resolution_decisions_append_only ON resolution_decisions;
DROP TRIGGER resolution_proposals_append_only ON resolution_proposals;
DROP TRIGGER collision_claim_matches_append_only ON collision_claim_matches;
DROP TRIGGER semantic_claims_append_only ON semantic_claims;
DROP TRIGGER intent_revisions_append_only ON intent_revisions;
DROP TRIGGER repository_connections_append_only ON repository_connections;

ALTER TABLE events DROP CONSTRAINT events_check;
ALTER TABLE events ADD CONSTRAINT events_check CHECK ((event_type, entity_type) IN (
    ('project.created', 'project'),
    ('project.member_added', 'project'),
    ('objective.revision_created', 'objective'),
    ('blueprint.revision_proposed', 'blueprint'),
    ('blueprint.revision_activated', 'blueprint'),
    ('blueprint.revision_rejected', 'blueprint'),
    ('charter.accepted', 'charter_decision'),
    ('charter.rejected', 'charter_decision')
));

DROP TABLE evidence_references;
DROP TABLE resolution_decisions;
DROP TABLE resolution_proposals;
DROP TABLE collision_claim_matches;
DROP TABLE collision_assessments;
DROP TABLE semantic_claims;
ALTER TABLE intents DROP CONSTRAINT intents_current_revision_fkey;
DROP TABLE intent_revisions;
DROP TABLE intents;
DROP TABLE participants;
DROP TABLE repository_connections;
