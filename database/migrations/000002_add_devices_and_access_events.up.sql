BEGIN;

-- ---------------------------------------------------------------------------
-- Enums
-- ---------------------------------------------------------------------------
CREATE TYPE device_command_status_enum AS ENUM ('queued','sent','acked','failed');
CREATE TYPE device_command_kind_enum   AS ENUM ('open_door','reboot','info','repull_logs','check');
-- denied_* are reserved for refused attempts once the device spike shows how the AI Face
-- Magnum reports them; ATTLOG only carries punches the device accepted.
CREATE TYPE access_verdict_enum        AS ENUM ('allowed','allowed_but_expired','unknown','denied_expired','denied_unknown');

-- ---------------------------------------------------------------------------
-- devices  (one eSSL terminal at a gym, identified by the serial it sends as ?SN=)
-- ---------------------------------------------------------------------------
-- The *Stamp columns are the ADMS upload cursors the device reports; handing them back in the
-- handshake makes it send only newer records. They are opaque strings, not numbers we compute.
CREATE TABLE devices (
    id              SERIAL PRIMARY KEY,
    "gymId"         INT NOT NULL REFERENCES gyms(id),
    "serialNumber"  VARCHAR(64) NOT NULL,
    "name"          VARCHAR(80) NOT NULL,
    "firmware"      VARCHAR(64),
    "pushVersion"   VARCHAR(32),
    "lastSeenAt"    TIMESTAMPTZ,
    "lastIp"        VARCHAR(64),
    "attStamp"      VARCHAR(32),
    "opStamp"       VARCHAR(32),
    "photoStamp"    VARCHAR(32),
    "deletedAt"     TIMESTAMPTZ,
    "createdAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_devices_id_gym UNIQUE (id,"gymId")
);
-- A serial belongs to one live device across all gyms: two gyms claiming the same terminal
-- would otherwise split its punches.
CREATE UNIQUE INDEX uq_devices_serial ON devices (UPPER("serialNumber")) WHERE "deletedAt" IS NULL;
CREATE TRIGGER set_timestamp BEFORE UPDATE ON devices
FOR EACH ROW EXECUTE FUNCTION trigger_set_timestamp();

-- ---------------------------------------------------------------------------
-- device_commands  (server → device queue, delivered on the device's next heartbeat)
-- ---------------------------------------------------------------------------
-- queued → sent (handed out on /iclock/getrequest) → acked | failed (reported on
-- /iclock/devicecmd). A sent command with no answer is handed out again after a timeout.
CREATE TABLE device_commands (
    id              BIGSERIAL PRIMARY KEY,
    "gymId"         INT NOT NULL,
    "deviceId"      INT NOT NULL,
    "kind"          device_command_kind_enum NOT NULL,
    "command"       TEXT NOT NULL,
    "status"        device_command_status_enum NOT NULL DEFAULT 'queued',
    "attempts"      INT NOT NULL DEFAULT 0,
    "returnCode"    INT,
    "requestedBy"   INT REFERENCES users(id),
    "sentAt"        TIMESTAMPTZ,
    "finishedAt"    TIMESTAMPTZ,
    "createdAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_device_commands_device FOREIGN KEY ("deviceId","gymId") REFERENCES devices(id,"gymId")
);
CREATE INDEX idx_device_commands_pending ON device_commands ("deviceId","id") WHERE "status" IN ('queued','sent');
CREATE TRIGGER set_timestamp BEFORE UPDATE ON device_commands
FOR EACH ROW EXECUTE FUNCTION trigger_set_timestamp();

-- ---------------------------------------------------------------------------
-- access_events  (one punch at a device: the gym's check-in log)
-- ---------------------------------------------------------------------------
-- pin is kept as sent even when it matches no member (memberId NULL, verdict unknown). The
-- unique key makes ingest idempotent: a device re-sends a batch it got no OK for, and the
-- repeat is dropped instead of doubling check-ins.
CREATE TABLE access_events (
    id              BIGSERIAL PRIMARY KEY,
    "gymId"         INT NOT NULL,
    "deviceId"      INT NOT NULL,
    "memberId"      INT,
    "pin"           VARCHAR(32) NOT NULL,
    "punchedAt"     TIMESTAMPTZ NOT NULL,
    "punchStatus"   INT,
    "verifyCode"    INT,
    "verifyMethod"  VARCHAR(16) NOT NULL,
    "verdict"       access_verdict_enum NOT NULL,
    "raw"           TEXT NOT NULL,
    "createdAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_access_events_device FOREIGN KEY ("deviceId","gymId") REFERENCES devices(id,"gymId"),
    CONSTRAINT fk_access_events_member FOREIGN KEY ("memberId","gymId") REFERENCES members(id,"gymId"),
    CONSTRAINT uq_access_events_punch UNIQUE ("deviceId","pin","punchedAt")
);
CREATE INDEX idx_access_events_gym_time    ON access_events ("gymId","punchedAt" DESC, id DESC);
CREATE INDEX idx_access_events_member_time ON access_events ("memberId","punchedAt" DESC) WHERE "memberId" IS NOT NULL;

-- ---------------------------------------------------------------------------
-- device_raw_messages  (every /iclock request, verbatim, for protocol debugging)
-- ---------------------------------------------------------------------------
-- eSSL publishes no ADMS spec, so this is how the device's real behaviour is learnt and how an
-- installer's mistyped serial is spotted: unknown serials are kept too, with deviceId NULL.
-- Bodies are truncated; rows older than 30 days are purged.
CREATE TABLE device_raw_messages (
    id              BIGSERIAL PRIMARY KEY,
    "deviceId"      INT REFERENCES devices(id),
    "serialNumber"  VARCHAR(64),
    "method"        VARCHAR(8) NOT NULL,
    "path"          TEXT NOT NULL,
    "query"         TEXT,
    "body"          TEXT,
    "bodyBytes"     INT NOT NULL DEFAULT 0,
    "status"        INT,
    "response"      TEXT,
    "remoteIp"      VARCHAR(64),
    "createdAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_device_raw_messages_serial_time ON device_raw_messages (UPPER("serialNumber"), "createdAt" DESC);
CREATE INDEX idx_device_raw_messages_time ON device_raw_messages ("createdAt");

COMMIT;
