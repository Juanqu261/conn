-- Repository and Participant operation support.
--
-- PostgreSQL unique constraints treat NULL values as distinct. Conn resolves an
-- omitted local session reference as one stable participant identity, so this
-- expression index coalesces that deliberate null-equivalent value.

-- +goose Up

CREATE UNIQUE INDEX participants_identity_idx
    ON participants (project_id, human_id, adapter_kind, COALESCE(local_session_ref, ''));

-- +goose Down
-- Development only. Automation never runs down migrations.

DROP INDEX participants_identity_idx;
