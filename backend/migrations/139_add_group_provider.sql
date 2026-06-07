ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS provider VARCHAR(50) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_groups_provider ON groups(provider);
