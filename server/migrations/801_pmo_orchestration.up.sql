ALTER TABLE pmo_sync_config
    ADD COLUMN orchestration_squad_id uuid,
    ADD COLUMN orchestration_issue_id uuid;
