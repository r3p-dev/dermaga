-- The restic password has three states (ARCHITECTURE v2 D62): none ('' in
-- restic_password), unconfirmed (set, confirmed_at 0) and confirmed. Passwords
-- generated silently by earlier versions start as unconfirmed.
ALTER TABLE settings ADD COLUMN restic_password_confirmed_at INTEGER NOT NULL DEFAULT 0; -- unix seconds
