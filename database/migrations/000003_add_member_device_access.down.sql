BEGIN;

DROP TABLE IF EXISTS biometric_templates;
DROP TABLE IF EXISTS member_device_access;

DROP INDEX IF EXISTS idx_device_commands_member;
ALTER TABLE device_commands DROP CONSTRAINT IF EXISTS fk_device_commands_member;
ALTER TABLE device_commands DROP COLUMN IF EXISTS "targetState";
ALTER TABLE device_commands DROP COLUMN IF EXISTS "memberId";
DROP TYPE IF EXISTS device_access_state_enum;

-- Enum values cannot be dropped; rebuild both enums without the added ones.
DELETE FROM device_commands WHERE "kind" IN ('sync_user','delete_user','restore_template');
UPDATE device_commands SET "status"='failed' WHERE "status"='superseded';

DROP INDEX IF EXISTS idx_device_commands_pending;
ALTER TYPE device_command_kind_enum RENAME TO device_command_kind_enum_old;
CREATE TYPE device_command_kind_enum AS ENUM ('open_door','reboot','info','repull_logs','check');
ALTER TABLE device_commands ALTER COLUMN "kind" TYPE device_command_kind_enum USING "kind"::text::device_command_kind_enum;
DROP TYPE device_command_kind_enum_old;

ALTER TABLE device_commands ALTER COLUMN "status" DROP DEFAULT;
ALTER TYPE device_command_status_enum RENAME TO device_command_status_enum_old;
CREATE TYPE device_command_status_enum AS ENUM ('queued','sent','acked','failed');
ALTER TABLE device_commands ALTER COLUMN "status" TYPE device_command_status_enum USING "status"::text::device_command_status_enum;
ALTER TABLE device_commands ALTER COLUMN "status" SET DEFAULT 'queued';
DROP TYPE device_command_status_enum_old;
CREATE INDEX idx_device_commands_pending ON device_commands ("deviceId","id") WHERE "status" IN ('queued','sent');

COMMIT;
