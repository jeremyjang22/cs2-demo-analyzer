// Atlas reads schema/schema.sql as the desired state and diffs it against the
// database to produce migrations. sqlc reads the same file to generate Go, so
// the schema is written once and both tools agree by construction.

variable "url" {
  type    = string
  default = getenv("DATABASE_URL")
}

// A scratch database Atlas uses to compute diffs. Neon's free tier allows
// extra databases on the same project; point this at one that holds nothing
// you care about, because Atlas will drop and recreate objects in it.
variable "dev_url" {
  type    = string
  default = getenv("DATABASE_DEV_URL")
}

env "neon" {
  src = "file://schema/schema.sql"
  url = var.url
  dev = var.dev_url

  // Manage only the public schema. Neon provisions its own "neon_auth" schema
  // in every database, and Atlas refuses to use a dev database that is not
  // clean ("connected database is not clean: found schema neon_auth").
  // Scoping here rather than dropping it keeps Neon's own feature intact, and
  // stops a diff from ever proposing to delete something Atlas does not own.
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
