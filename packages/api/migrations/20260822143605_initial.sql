-- Create enum type "demo_status"
CREATE TYPE "demo_status" AS ENUM ('uploading', 'queued', 'parsing', 'ready', 'failed');
-- Create enum type "demo_visibility"
CREATE TYPE "demo_visibility" AS ENUM ('private', 'link', 'public');
-- Create enum type "job_state"
CREATE TYPE "job_state" AS ENUM ('queued', 'running', 'done', 'failed');
-- Create "users" table
CREATE TABLE "users" (
  "steamid" bigint NOT NULL,
  "display_name" text NOT NULL,
  "avatar_url" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "last_seen_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("steamid")
);
-- Create "demos" table
CREATE TABLE "demos" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "sha256" character(64) NOT NULL,
  "uploaded_by" bigint NOT NULL,
  "original_filename" text NOT NULL,
  "size_bytes" bigint NOT NULL,
  "status" "demo_status" NOT NULL DEFAULT 'uploading',
  "visibility" "demo_visibility" NOT NULL DEFAULT 'private',
  "map" text NULL,
  "tick_rate" real NULL,
  "rounds" integer NULL,
  "complete" boolean NULL,
  "schema_version" text NULL,
  "collector_version" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "parsed_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "demos_sha256_key" UNIQUE ("sha256"),
  CONSTRAINT "demos_uploaded_by_fkey" FOREIGN KEY ("uploaded_by") REFERENCES "users" ("steamid") ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "demos_status_idx" to table: "demos"
CREATE INDEX "demos_status_idx" ON "demos" ("status");
-- Create index "demos_uploaded_by_idx" to table: "demos"
CREATE INDEX "demos_uploaded_by_idx" ON "demos" ("uploaded_by", "created_at" DESC);
-- Create "demo_players" table
CREATE TABLE "demo_players" (
  "demo_id" uuid NOT NULL,
  "steamid" bigint NOT NULL,
  "name" text NOT NULL,
  "team" smallint NOT NULL,
  "kills" integer NOT NULL DEFAULT 0,
  "deaths" integer NOT NULL DEFAULT 0,
  "assists" integer NOT NULL DEFAULT 0,
  PRIMARY KEY ("demo_id", "steamid"),
  CONSTRAINT "demo_players_demo_id_fkey" FOREIGN KEY ("demo_id") REFERENCES "demos" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "demo_players_steamid_idx" to table: "demo_players"
CREATE INDEX "demo_players_steamid_idx" ON "demo_players" ("steamid");
-- Create "jobs" table
CREATE TABLE "jobs" (
  "id" bigserial NOT NULL,
  "demo_id" uuid NOT NULL,
  "state" "job_state" NOT NULL DEFAULT 'queued',
  "attempts" integer NOT NULL DEFAULT 0,
  "last_error" text NULL,
  "locked_at" timestamptz NULL,
  "locked_by" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "jobs_demo_id_fkey" FOREIGN KEY ("demo_id") REFERENCES "demos" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "jobs_claimable_idx" to table: "jobs"
CREATE INDEX "jobs_claimable_idx" ON "jobs" ("created_at") WHERE (state = 'queued'::job_state);
-- Create index "jobs_one_live_per_demo_idx" to table: "jobs"
CREATE UNIQUE INDEX "jobs_one_live_per_demo_idx" ON "jobs" ("demo_id") WHERE (state = ANY (ARRAY['queued'::job_state, 'running'::job_state]));
