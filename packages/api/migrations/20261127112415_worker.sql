-- Modify "jobs" table
ALTER TABLE "jobs" ADD COLUMN "started_at" timestamptz NULL, ADD COLUMN "finished_at" timestamptz NULL, ADD COLUMN "steps" jsonb NULL;
-- Create "workers" table
CREATE TABLE "workers" (
  "name" text NOT NULL,
  "version" text NOT NULL,
  "started_at" timestamptz NOT NULL,
  "seen_at" timestamptz NOT NULL,
  "job_id" bigint NULL,
  PRIMARY KEY ("name")
);
