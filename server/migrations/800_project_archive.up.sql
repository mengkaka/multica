ALTER TABLE project
    ADD COLUMN archived_at timestamptz,
    ADD COLUMN archived_by uuid;
