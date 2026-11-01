BEGIN;

DROP TABLE IF EXISTS device_raw_messages;
DROP TABLE IF EXISTS access_events;
DROP TABLE IF EXISTS device_commands;
DROP TABLE IF EXISTS devices;

DROP TYPE IF EXISTS access_verdict_enum;
DROP TYPE IF EXISTS device_command_kind_enum;
DROP TYPE IF EXISTS device_command_status_enum;

COMMIT;
