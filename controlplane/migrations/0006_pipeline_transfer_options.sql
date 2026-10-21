ALTER TABLE pipelines
  ADD COLUMN IF NOT EXISTS transfer_options jsonb NOT NULL DEFAULT '{}'::jsonb;
