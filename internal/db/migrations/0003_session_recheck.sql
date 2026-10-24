-- Encrypted OIDC refresh token, used to re-check group membership, and when
-- that was last done ('' = never).
ALTER TABLE sessions ADD COLUMN refresh_token TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN checked_at TEXT NOT NULL DEFAULT '';
