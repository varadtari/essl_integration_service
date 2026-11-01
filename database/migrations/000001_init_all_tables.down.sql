BEGIN;

DROP TABLE IF EXISTS internal_errors;
DROP TABLE IF EXISTS debug_activity_logs_api;
DROP TABLE IF EXISTS payments;
DROP TABLE IF EXISTS memberships;
DROP TABLE IF EXISTS members;
DROP TABLE IF EXISTS plans;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS gyms;

DROP TYPE IF EXISTS gym_billing_status_enum;
DROP TYPE IF EXISTS payment_method_enum;
DROP TYPE IF EXISTS user_role_enum;

DROP FUNCTION IF EXISTS trigger_set_timestamp();

COMMIT;
