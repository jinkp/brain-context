-- Add encrypted embed API key to projects
ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS embed_api_key_enc text;

-- Invite codes table
-- An invite allows a developer to join a project and download its config
-- (including the encrypted embed API key) using only an invite code.
CREATE TABLE IF NOT EXISTS invite_codes (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_id  uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    code        text NOT NULL UNIQUE,
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_invite_codes_code ON invite_codes(code);
CREATE INDEX IF NOT EXISTS idx_invite_codes_project ON invite_codes(project_id);
