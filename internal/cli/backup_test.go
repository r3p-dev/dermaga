package cli

import (
	"testing"

	"github.com/r3p-dev/dermaga/internal/backup"
)

func TestMergeBackupSettings(t *testing.T) {
	cur := backup.Settings{
		Repository: "/home/server/.local/share/dermaga/restic", Password: "restic-pass",
		S3Region: "auto", S3AccessKey: "AK", S3SecretKey: "••••••••", Configured: true,
	}
	in := backup.Settings{Repository: "s3:https://h/b2", S3Endpoint: "s3.example.com", S3Bucket: "bkt", S3Region: "eu", S3AccessKey: "AK2"}

	// Only the region changes; secrets are never sent back.
	got := mergeBackupSettings(cur, in, map[string]bool{"s3-region": true})
	if got.Password != "" || got.S3SecretKey != "" {
		t.Errorf("secrets echoed back: %+v", got)
	}
	if got.S3Region != "eu" || got.Repository != cur.Repository || got.S3AccessKey != "AK" || got.S3Endpoint != "" {
		t.Errorf("region-only change: %+v", got)
	}

	// New endpoint/bucket without --repo: the server derives the repository.
	got = mergeBackupSettings(cur, in, map[string]bool{"s3-endpoint": true, "s3-bucket": true, "s3-access-key": true})
	if got.Repository != "" || got.S3Endpoint != "s3.example.com" || got.S3Bucket != "bkt" || got.S3AccessKey != "AK2" {
		t.Errorf("S3 change: %+v", got)
	}

	// An explicit --repo wins.
	got = mergeBackupSettings(cur, in, map[string]bool{"repo": true, "s3-bucket": true})
	if got.Repository != "s3:https://h/b2" {
		t.Errorf("explicit repo: %+v", got)
	}
}
