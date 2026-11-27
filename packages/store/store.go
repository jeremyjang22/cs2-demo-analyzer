// Package store reads and writes the R2 bucket the site serves demos from,
// through R2's S3-compatible API.
//
// The Worker reads the same bucket through its binding (wrangler.jsonc); this
// is the other door in, for Go: demo-catalog and demo-export publish through
// it, and the upload worker will. Keys are the published paths -
// data/<name>/movement.json is served at /data/<name>/movement.json - so a key
// written here is a URL on the site, and nothing translates between the two.
//
// The credentials are an R2 API token scoped to this one bucket (Object Read &
// Write), never the account's: a leaked key reaches one bucket of public-ish
// JSON, not the Cloudflare account.
package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ErrNotFound is returned by Get for a key the bucket does not hold.
var ErrNotFound = errors.New("not in the bucket")

// Config is what reaching the bucket takes. The names are the environment
// variables' (docs/uploads.md), read the same on the PC and on Northflank.
type Config struct {
	AccountID       string // R2_ACCOUNT_ID
	AccessKeyID     string // R2_ACCESS_KEY_ID
	SecretAccessKey string // R2_SECRET_ACCESS_KEY
	Bucket          string // R2_BUCKET
}

// ConfigFromEnv reads Config, naming every variable that is missing at once
// rather than failing on the first.
func ConfigFromEnv() (Config, error) {
	c := Config{
		AccountID:       strings.TrimSpace(os.Getenv("R2_ACCOUNT_ID")),
		AccessKeyID:     strings.TrimSpace(os.Getenv("R2_ACCESS_KEY_ID")),
		SecretAccessKey: strings.TrimSpace(os.Getenv("R2_SECRET_ACCESS_KEY")),
		Bucket:          strings.TrimSpace(os.Getenv("R2_BUCKET")),
	}
	var missing []string
	for name, v := range map[string]string{
		"R2_ACCOUNT_ID": c.AccountID, "R2_ACCESS_KEY_ID": c.AccessKeyID,
		"R2_SECRET_ACCESS_KEY": c.SecretAccessKey, "R2_BUCKET": c.Bucket,
	} {
		if v == "" {
			missing = append(missing, name)
		}
	}
	slices.Sort(missing)
	if len(missing) > 0 {
		return c, fmt.Errorf("R2 is not configured, missing %s (see docs/uploads.md)", strings.Join(missing, ", "))
	}
	return c, nil
}

// Store is one bucket.
type Store struct {
	client *minio.Client
	bucket string
}

func Open(c Config) (*Store, error) {
	client, err := minio.New(c.AccountID+".r2.cloudflarestorage.com", &minio.Options{
		Creds:  credentials.NewStaticV4(c.AccessKeyID, c.SecretAccessKey, ""),
		Secure: true,
		// R2 has one region and calls it "auto". Naming it stops the client
		// asking the bucket for its location before the first request.
		Region: "auto",
	})
	if err != nil {
		return nil, err
	}
	return &Store{client: client, bucket: c.Bucket}, nil
}

// FromEnv is ConfigFromEnv then Open.
func FromEnv() (*Store, error) {
	c, err := ConfigFromEnv()
	if err != nil {
		return nil, err
	}
	return Open(c)
}

func (s *Store) Bucket() string { return s.bucket }

// Put writes body under key, replacing what was there.
func (s *Store) Put(ctx context.Context, key string, body []byte, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(body), int64(len(body)),
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("put %s: %w", key, err)
	}
	return nil
}

// Get reads a whole object. ErrNotFound if there is none.
func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, s.wrap("get", key, err)
	}
	defer obj.Close()
	b, err := io.ReadAll(obj)
	if err != nil {
		return nil, s.wrap("get", key, err)
	}
	return b, nil
}

// Exists reports whether key is in the bucket.
func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.Size(ctx, key)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// Size is an object's length in bytes. ErrNotFound if there is none.
func (s *Store) Size(ctx context.Context, key string) (int64, error) {
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return 0, s.wrap("stat", key, err)
	}
	return info.Size, nil
}

// Delete removes an object. Deleting one that is not there is not an error.
func (s *Store) Delete(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return s.wrap("delete", key, err)
	}
	return nil
}

// PresignPut is a URL that lets whoever holds it PUT exactly size bytes to
// key, and nothing else, until it expires - how a browser uploads a demo
// straight to the bucket without holding a credential. The length is part of
// the signature, so the same URL used for a different file is refused by R2
// itself; the API still checks the stored size afterwards.
func (s *Store) PresignPut(ctx context.Context, key string, size int64, expires time.Duration) (string, error) {
	u, err := s.client.PresignHeader(ctx, http.MethodPut, s.bucket, key, expires, nil,
		http.Header{"Content-Length": {strconv.FormatInt(size, 10)}})
	if err != nil {
		return "", fmt.Errorf("presign %s: %w", key, err)
	}
	return u.String(), nil
}

func (s *Store) wrap(op, key string, err error) error {
	if code := minio.ToErrorResponse(err).Code; code == "NoSuchKey" || code == "NotFound" {
		return fmt.Errorf("%s %s: %w", op, key, ErrNotFound)
	}
	return fmt.Errorf("%s %s: %w", op, key, err)
}
