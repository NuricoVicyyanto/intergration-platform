ALTER TABLE connections
  DROP CONSTRAINT IF EXISTS connections_driver_check;

ALTER TABLE connections
  ADD CONSTRAINT connections_driver_check
  CHECK (driver IN ('postgres', 'mysql', 'mssql', 'oracle', 'mongodb'));
