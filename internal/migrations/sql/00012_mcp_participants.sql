-- Participants of MCP clients other than Kiro.
--
-- The generic mcp adapter kind attributes work from any MCP coding agent. It
-- changes attribution only, never authority.

-- +goose Up

ALTER TABLE participants DROP CONSTRAINT participants_adapter_kind_check;
ALTER TABLE participants ADD CONSTRAINT participants_adapter_kind_check
    CHECK (adapter_kind IN ('kiro', 'mcp', 'cli', 'ci', 'github', 'other_registered_adapter'));

-- +goose Down
-- Development only. Automation never runs down migrations.

ALTER TABLE participants DROP CONSTRAINT participants_adapter_kind_check;
ALTER TABLE participants ADD CONSTRAINT participants_adapter_kind_check
    CHECK (adapter_kind IN ('kiro', 'cli', 'ci', 'github', 'other_registered_adapter'));