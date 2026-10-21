-- Existing databases may already have applied 0002 before secret_ref was made
-- optional for local/demo password connections.
ALTER TABLE connections ALTER COLUMN secret_ref DROP NOT NULL;
