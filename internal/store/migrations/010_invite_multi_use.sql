-- Allow invite codes to be used multiple times (default 1 = single-use).
-- Replaces the binary used_at column with a use_count counter and max_uses cap.

ALTER TABLE invite_codes
    ADD COLUMN IF NOT EXISTS max_uses  int NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS use_count int NOT NULL DEFAULT 0;

-- Backfill: codes that were already redeemed get use_count = 1
UPDATE invite_codes SET use_count = 1 WHERE used_at IS NOT NULL;

-- Drop the old binary column (no longer needed)
ALTER TABLE invite_codes DROP COLUMN IF EXISTS used_at;
