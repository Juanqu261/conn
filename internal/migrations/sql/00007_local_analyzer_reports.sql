-- Bounded, source-free local analyzer report metadata.
-- +goose Up

CREATE TABLE intent_analysis_reports (
    project_id      uuid        NOT NULL,
    intent_id       uuid        NOT NULL,
    intent_revision integer     NOT NULL CHECK (intent_revision > 0),
    canonical_remote text       NOT NULL CHECK (canonical_remote ~ '^[a-z0-9][a-z0-9.-]*(/[A-Za-z0-9._~-]+)+$'),
    diagnostics     jsonb       NOT NULL CHECK (jsonb_typeof(diagnostics) = 'array' AND jsonb_array_length(diagnostics) <= 100),
    observed_at     timestamptz NOT NULL,
    PRIMARY KEY (project_id, intent_id, intent_revision),
    FOREIGN KEY (project_id, intent_id, intent_revision)
        REFERENCES intent_revisions (project_id, intent_id, revision)
);
CREATE TRIGGER intent_analysis_reports_append_only BEFORE UPDATE OR DELETE ON intent_analysis_reports
    FOR EACH ROW EXECUTE FUNCTION conn_reject_mutation();

-- +goose Down
-- Development only. Automation never runs down migrations.
DROP TRIGGER intent_analysis_reports_append_only ON intent_analysis_reports;
DROP TABLE intent_analysis_reports;
