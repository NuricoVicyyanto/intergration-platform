ALTER TABLE pipelines
  ADD COLUMN IF NOT EXISTS source_filter jsonb NOT NULL DEFAULT '{}'::jsonb;
