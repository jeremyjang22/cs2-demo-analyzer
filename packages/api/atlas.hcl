// Atlas reads schema/schema.sql as the desired state and diffs it against the
// database to produce migrations. sqlc reads the same file to generate Go, so
// the schema is written once and both tools agree by construction.

variable "url" {
  type    = string
  default = getenv("DATABASE_URL")
}

// A scratch database Atlas uses to compute diffs: it drops and recreates
// objects in it, so it must never be the real one. docker://postgres/17/dev
// (see .env.example) has Atlas start a throwaway container per run, the same
// major version as production, so no hosted database is spent on it.
variable "dev_url" {
  type    = string
  default = getenv("DATABASE_DEV_URL")
}

env "db" {
  src = "file://schema/schema.sql"
  url = var.url
  dev = var.dev_url

  // Manage only the public schema. Supabase fills every database with its own
  // (auth, storage, realtime, extensions, ...), and a diff that saw them would
  // propose deleting things Atlas does not own. The connection strings end in
  // ?search_path=public for the same reason.
  schemas = ["public"]
  migration {
    dir = "file://migrations"

    // Keep Atlas's own revision history in public alongside the tables it
    // manages. By default it wants a dedicated atlas_schema_revisions schema,
    // which the connection cannot reach once search_path scopes it to public -
    // it fails with "relation atlas_schema_revisions.atlas_schema_revisions
    // does not exist".
    revisions_schema = "public"
  }
  format {
    migrate {
      diff = "{{ sql . \"  \" }}"
    }
  }
}
