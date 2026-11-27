-- The catalogue moves into the database (docs/uploads.md, phase A).
-- name is added NOT NULL with no default: demos held no rows when this was
-- written (checked against production 2026-11-26), and every row from here
-- on is created with a name.
-- Create enum type "demo_kind"
CREATE TYPE "demo_kind" AS ENUM ('pug', 'match');
-- Modify "demos" table
ALTER TABLE "demos" ADD CONSTRAINT "demos_name_check" CHECK (name ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$'::text), ADD CONSTRAINT "demos_sample_is_public" CHECK ((NOT sample) OR (visibility = 'public'::demo_visibility)), ALTER COLUMN "sha256" DROP NOT NULL, ALTER COLUMN "uploaded_by" DROP NOT NULL, ALTER COLUMN "original_filename" DROP NOT NULL, ALTER COLUMN "size_bytes" DROP NOT NULL, ADD COLUMN "name" text NOT NULL, ADD COLUMN "owner" bigint NULL, ADD COLUMN "kind" "demo_kind" NULL, ADD COLUMN "event" text NULL, ADD COLUMN "played_on" date NULL, ADD COLUMN "title" text NULL, ADD COLUMN "sample" boolean NOT NULL DEFAULT false, ADD COLUMN "summary" jsonb NULL, ADD CONSTRAINT "demos_name_key" UNIQUE ("name");
