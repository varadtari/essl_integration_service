BEGIN;

-- ---------------------------------------------------------------------------
-- Shared updatedAt trigger
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION trigger_set_timestamp()
RETURNS TRIGGER AS $$
BEGIN
  NEW."updatedAt" = NOW();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- ---------------------------------------------------------------------------
-- Enums
-- ---------------------------------------------------------------------------
CREATE TYPE user_role_enum          AS ENUM ('superadmin','owner','staff');
CREATE TYPE payment_method_enum     AS ENUM ('cash','upi');
CREATE TYPE gym_billing_status_enum AS ENUM ('trial','paid','unpaid');

-- ---------------------------------------------------------------------------
-- gyms  (tenant: every other business table hangs off a gym)
-- ---------------------------------------------------------------------------
-- timezone drives "today" for expiry and the conversion of device-local punch times, so it
-- is stored per gym rather than assumed to be IST.
CREATE TABLE gyms (
    id              SERIAL PRIMARY KEY,
    "name"          VARCHAR(120) NOT NULL,
    "timezone"      VARCHAR(64) NOT NULL DEFAULT 'Asia/Kolkata',
    "graceDays"     INT NOT NULL DEFAULT 0 CHECK ("graceDays" >= 0),
    "blockOnExpiry" BOOLEAN NOT NULL DEFAULT TRUE,
    "billingStatus" gym_billing_status_enum NOT NULL DEFAULT 'trial',
    "createdAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TRIGGER set_timestamp BEFORE UPDATE ON gyms
FOR EACH ROW EXECUTE FUNCTION trigger_set_timestamp();

-- ---------------------------------------------------------------------------
-- users  (portal logins: the client's superadmins, and each gym's owner/staff)
-- ---------------------------------------------------------------------------
-- A superadmin belongs to no gym and every other role belongs to exactly one; the CHECK keeps
-- a gym-less owner from existing, since tenancy is enforced from the session's gymId.
CREATE TABLE users (
    id              SERIAL PRIMARY KEY,
    "gymId"         INT REFERENCES gyms(id),
    "name"          VARCHAR(120) NOT NULL,
    "email"         VARCHAR(254) NOT NULL,
    "passwordHash"  TEXT NOT NULL,
    "role"          user_role_enum NOT NULL,
    "active"        BOOLEAN NOT NULL DEFAULT TRUE,
    "createdAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_users_role_gym CHECK (("role" = 'superadmin') = ("gymId" IS NULL))
);
CREATE UNIQUE INDEX uq_users_email ON users (LOWER("email"));
CREATE TRIGGER set_timestamp BEFORE UPDATE ON users
FOR EACH ROW EXECUTE FUNCTION trigger_set_timestamp();

-- ---------------------------------------------------------------------------
-- plans  (membership plans a gym sells; the prototype hardcoded 1/3/6/12 months)
-- ---------------------------------------------------------------------------
-- Money is integer paise throughout, never float rupees.
CREATE TABLE plans (
    id              SERIAL PRIMARY KEY,
    "gymId"         INT NOT NULL REFERENCES gyms(id),
    "name"          VARCHAR(60) NOT NULL,
    "months"        INT NOT NULL CHECK ("months" > 0),
    "pricePaise"    BIGINT NOT NULL CHECK ("pricePaise" >= 0),
    "active"        BOOLEAN NOT NULL DEFAULT TRUE,
    "createdAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_plans_gym_name UNIQUE ("gymId","name")
);
CREATE TRIGGER set_timestamp BEFORE UPDATE ON plans
FOR EACH ROW EXECUTE FUNCTION trigger_set_timestamp();

-- ---------------------------------------------------------------------------
-- members
-- ---------------------------------------------------------------------------
-- devicePin is the user id written to the eSSL device and echoed back on every punch, so it is
-- unique per gym and never reused: a soft-deleted member keeps their pin, otherwise old punches
-- would resolve to the wrong person. (id, gymId) is unique so child tables can reference both
-- and a row can never point at another gym's member.
CREATE TABLE members (
    id              SERIAL PRIMARY KEY,
    "gymId"         INT NOT NULL REFERENCES gyms(id),
    "devicePin"     INT NOT NULL CHECK ("devicePin" > 0),
    "name"          VARCHAR(120) NOT NULL,
    "phone"         VARCHAR(15) NOT NULL,
    "deletedAt"     TIMESTAMPTZ,
    "createdAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_members_gym_pin UNIQUE ("gymId","devicePin"),
    CONSTRAINT uq_members_id_gym  UNIQUE (id,"gymId")
);
CREATE INDEX idx_members_gym_name ON members ("gymId","name") WHERE "deletedAt" IS NULL;
CREATE TRIGGER set_timestamp BEFORE UPDATE ON members
FOR EACH ROW EXECUTE FUNCTION trigger_set_timestamp();

-- ---------------------------------------------------------------------------
-- memberships  (one row per purchased term; a member's expiry is their latest endDate)
-- ---------------------------------------------------------------------------
-- Dates, not timestamps: a term ends on a calendar day in the gym's timezone.
CREATE TABLE memberships (
    id              SERIAL PRIMARY KEY,
    "gymId"         INT NOT NULL,
    "memberId"      INT NOT NULL,
    "planId"        INT NOT NULL REFERENCES plans(id),
    "startDate"     DATE NOT NULL,
    "endDate"       DATE NOT NULL,
    "createdBy"     INT REFERENCES users(id),
    "createdAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_memberships_member FOREIGN KEY ("memberId","gymId") REFERENCES members(id,"gymId"),
    CONSTRAINT chk_memberships_dates CHECK ("endDate" > "startDate")
);
CREATE INDEX idx_memberships_member_end ON memberships ("memberId","endDate" DESC);
CREATE TRIGGER set_timestamp BEFORE UPDATE ON memberships
FOR EACH ROW EXECUTE FUNCTION trigger_set_timestamp();

-- ---------------------------------------------------------------------------
-- payments  (cash/UPI logged at the front desk)
-- ---------------------------------------------------------------------------
CREATE TABLE payments (
    id              SERIAL PRIMARY KEY,
    "gymId"         INT NOT NULL,
    "memberId"      INT NOT NULL,
    "membershipId"  INT NOT NULL REFERENCES memberships(id),
    "amountPaise"   BIGINT NOT NULL CHECK ("amountPaise" >= 0),
    "method"        payment_method_enum NOT NULL,
    "paidAt"        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "recordedBy"    INT REFERENCES users(id),
    "createdAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_payments_member FOREIGN KEY ("memberId","gymId") REFERENCES members(id,"gymId")
);
CREATE INDEX idx_payments_gym_paid_at ON payments ("gymId","paidAt" DESC);
CREATE TRIGGER set_timestamp BEFORE UPDATE ON payments
FOR EACH ROW EXECUTE FUNCTION trigger_set_timestamp();

-- ---------------------------------------------------------------------------
-- debug_activity_logs_api  (written by middleware.LogRequest, same shape as the template)
-- ---------------------------------------------------------------------------
CREATE TABLE debug_activity_logs_api (
    id              BIGSERIAL PRIMARY KEY,
    "xRequestId"    VARCHAR(256),
    "method"        VARCHAR(16),
    "path"          TEXT,
    "headers"       JSONB,
    "requestBody"   JSONB,
    "response"      TEXT,
    "status"        INT,
    "latency"       BIGINT,
    "userAgent"     TEXT,
    "createdAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ---------------------------------------------------------------------------
-- internal_errors  (written by misc.ProcessError)
-- ---------------------------------------------------------------------------
CREATE TABLE internal_errors (
    id               SERIAL PRIMARY KEY,
    "priority"       INT,
    "errorMessage"   TEXT,
    "additionalInfo" TEXT,
    "createdAt"      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt"      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TRIGGER set_timestamp BEFORE UPDATE ON internal_errors
FOR EACH ROW EXECUTE FUNCTION trigger_set_timestamp();

COMMIT;
