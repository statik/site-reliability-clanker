package runner

import (
	"fmt"
	"slices"
	"strings"
)

// s3Settings are the settings of the s3 backend of a job on AWS. Each must have a value:
// where the state is, and the locking and the encryption that platform/CONTRACT.md
// requires. Any other setting is refused, so that no setting of the local backend, such as
// path, reaches an s3 backend.
var s3Settings = []string{"bucket", "encrypt", "key", "region", "use_lockfile"}

// S3Backend returns the settings of the s3 backend for a layer of an environment on AWS:
// the key <env>/<layer>.tfstate in the bucket, with locking and encryption.
func S3Backend(bucket, region, env, layer string) map[string]string {
	return map[string]string{"bucket": bucket, "key": env + "/" + layer + ".tfstate",
		"region": region, "use_lockfile": "true", "encrypt": "true"}
}

// checkS3Backend refuses settings of an s3 backend that are not exactly s3Settings with
// values, or that turn off the locking or the encryption.
func checkS3Backend(settings map[string]string) error {
	for name := range settings {
		if !slices.Contains(s3Settings, name) {
			return fmt.Errorf("the s3 backend takes no setting %q: give exactly %s", name,
				strings.Join(s3Settings, ", "))
		}
	}
	for _, name := range s3Settings {
		if settings[name] == "" {
			return fmt.Errorf("the s3 backend has no value for %s: give exactly %s", name,
				strings.Join(s3Settings, ", "))
		}
	}
	for _, name := range []string{"encrypt", "use_lockfile"} {
		if settings[name] != "true" {
			return fmt.Errorf("the s3 backend has %s = %q: the state is always locked and "+
				"encrypted, so it must be \"true\"", name, settings[name])
		}
	}
	return nil
}
