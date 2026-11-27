-- Narrow CI credentials for the Conn pull-request check.
--
-- A CI credential reads one Project through an explicit route allowlist and
-- previews collisions for one RepositoryConnection. Only the SHA-256 hash of
-- its conn_ci_ token is stored. Rows change only to record throttled last use
-- and revocation.

-- +goose Up

CREATE TABLE ci_credentials (
    credential_id       uuid        PRIMARY KEY CHECK (uuid_extract_version(credential_id) = 7),
    project_id          uuid        NOT NULL,
    repository_id       uuid        NOT NULL,
    label               text        CHECK (label IS NULL OR char_length(label) BETWEEN 1 AND 200),
    token_sha256        bytea       NOT NULL UNIQUE CHECK (octet_length(token_sha256) = 32),
    created_by_human_id uuid        NOT NULL,
    created_at          timestamptz NOT NULL,
    expires_at          timestamptz NOT NULL,
    last_used_at        timestamptz,
    revoked_at          timestamptz,
    FOREIGN KEY (project_id, repository_id)
        REFERENCES repository_connections (project_id, repository_id),
    FOREIGN KEY (project_id, created_by_human_id)
        REFERENCES project_memberships (project_id, human_id),
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '365 days'),
    CHECK (last_used_at IS NULL OR last_used_at >= created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);
CREATE INDEX ci_credentials_project_idx ON ci_credentials (project_id, created_at DESC);

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
    ('combination.checked', 'combination_check'),
    ('ci_credential.created', 'ci_credential'),
    ('ci_credential.revoked', 'ci_credential')
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
    ('objective.imported', 'objective'),
    ('combination.checked', 'combination_check')
));
DROP TABLE ci_credentials;