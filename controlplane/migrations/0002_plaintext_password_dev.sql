-- Local/demo compatibility only. Production deployments should use secret_ref.
ALTER TABLE connections ADD COLUMN IF NOT EXISTS plaintext_password text;
ALTER TABLE connections ALTER COLUMN secret_ref DROP NOT NULL;
