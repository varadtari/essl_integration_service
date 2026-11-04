BEGIN;

-- New command kinds for keeping members on devices, and a status for queued commands replaced
-- by a newer one before the device fetched them (only the latest desired state is sent).
ALTER TYPE device_command_kind_enum ADD VALUE IF NOT EXISTS 'sync_user';
ALTER TYPE device_command_kind_enum ADD VALUE IF NOT EXISTS 'delete_user';
ALTER TYPE device_command_kind_enum ADD VALUE IF NOT EXISTS 'restore_template';
ALTER TYPE device_command_status_enum ADD VALUE IF NOT EXISTS 'superseded';

CREATE TYPE device_access_state_enum AS ENUM ('active','blocked','absent');

-- Which member a command is for and the access state it brings about once acknowledged, so an
-- ack can update member_device_access.
ALTER TABLE device_commands ADD COLUMN "memberId" INT;
ALTER TABLE device_commands ADD COLUMN "targetState" device_access_state_enum;
ALTER TABLE device_commands ADD CONSTRAINT fk_device_commands_member FOREIGN KEY ("memberId","gymId") REFERENCES members(id,"gymId");
CREATE INDEX idx_device_commands_member ON device_commands ("memberId","deviceId") WHERE "memberId" IS NOT NULL;

-- ---------------------------------------------------------------------------
-- member_device_access  (what each device should and does know about each member)
-- ---------------------------------------------------------------------------
-- desiredState comes from the membership (term + grace days, the gym's blockOnExpiry) and
-- actualState from acknowledged commands. The reconciler sends commands only where they
-- differ. payloadHash is the user record last pushed, so a rename is resent. failedAttempts
-- and lastError stop a command the device keeps refusing from being retried forever.
CREATE TABLE member_device_access (
    "gymId"          INT NOT NULL,
    "memberId"       INT NOT NULL,
    "deviceId"       INT NOT NULL,
    "desiredState"   device_access_state_enum NOT NULL,
    "actualState"    device_access_state_enum NOT NULL DEFAULT 'absent',
    "payloadHash"    VARCHAR(64),
    "lastCommandId"  BIGINT,
    "failedAttempts" INT NOT NULL DEFAULT 0,
    "lastError"      TEXT,
    "createdAt"      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt"      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY ("memberId","deviceId"),
    CONSTRAINT fk_member_device_access_member FOREIGN KEY ("memberId","gymId") REFERENCES members(id,"gymId"),
    CONSTRAINT fk_member_device_access_device FOREIGN KEY ("deviceId","gymId") REFERENCES devices(id,"gymId")
);
CREATE INDEX idx_member_device_access_gym ON member_device_access ("gymId");
CREATE TRIGGER set_timestamp BEFORE UPDATE ON member_device_access
FOR EACH ROW EXECUTE FUNCTION trigger_set_timestamp();

-- ---------------------------------------------------------------------------
-- biometric_templates  (face/fingerprint data a device uploaded, to restore it later)
-- ---------------------------------------------------------------------------
-- A blocked member is deleted from the device, which drops their templates there. The
-- device's own OPERLOG upload line is kept verbatim and replayed as "DATA UPDATE <line>" on
-- unblock, so renewing does not mean enrolling again. One row per (member, kind, slot).
-- These are biometric data: never returned by the API, only sent back to the gym's devices.
CREATE TABLE biometric_templates (
    id               BIGSERIAL PRIMARY KEY,
    "gymId"          INT NOT NULL,
    "memberId"       INT NOT NULL,
    "kind"           VARCHAR(16) NOT NULL,
    "slot"           VARCHAR(32) NOT NULL,
    "line"           TEXT NOT NULL,
    "sourceDeviceId" INT,
    "createdAt"      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt"      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_biometric_templates_member FOREIGN KEY ("memberId","gymId") REFERENCES members(id,"gymId"),
    CONSTRAINT uq_biometric_templates_slot UNIQUE ("memberId","kind","slot")
);
CREATE TRIGGER set_timestamp BEFORE UPDATE ON biometric_templates
FOR EACH ROW EXECUTE FUNCTION trigger_set_timestamp();

COMMIT;
