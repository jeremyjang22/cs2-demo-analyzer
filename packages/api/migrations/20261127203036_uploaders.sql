-- Create "uploaders" table
CREATE TABLE "uploaders" (
  "steamid" bigint NOT NULL,
  "note" text NULL,
  "added_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("steamid")
);
