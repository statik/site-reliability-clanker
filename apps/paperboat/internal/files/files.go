// Package files reads and writes the documents on the shared volume.
package files

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/domain"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/store"
)

// Directories inside the data directory.
const (
	ManifestDir    = "manifests"
	CertificateDir = "certificates"
)

// WriteManifest writes the manifest of a delivery and returns its path relative to dataDir.
func WriteManifest(dataDir string, info domain.ManifestInfo) (string, error) {
	relative := filepath.Join(ManifestDir, domain.ManifestName(info.DeliveryID))
	if err := writeAtomic(filepath.Join(dataDir, relative), domain.Manifest(info)); err != nil {
		return "", err
	}
	return relative, nil
}

// PathSetter records the manifest path of a delivery.
type PathSetter interface {
	SetManifestPath(ctx context.Context, id int64, path string) error
}

// IssueManifest writes the manifest of a job that just left the building and records
// where it is.
func IssueManifest(ctx context.Context, dataDir string, db PathSetter, d store.Delivery,
	now time.Time,
) error {
	relative, err := WriteManifest(dataDir, domain.ManifestInfo{
		DeliveryID: d.ID, Kind: d.Kind, RentalID: d.RentalID, Driver: d.Driver,
		WindowStart: d.WindowStart, WindowEnd: d.WindowEnd, Customer: d.Customer,
		Address: d.Address, Fish: d.Fish, Species: d.Species, Tank: d.Tank, IssuedAt: now,
	})
	if err != nil {
		return err
	}
	return db.SetManifestPath(ctx, d.ID, relative)
}

// WriteCertificate writes the health certificate of one fish.
func WriteCertificate(dataDir string, fishID int64, content string) error {
	name := fmt.Sprintf("fish-%d.txt", fishID)
	return writeAtomic(filepath.Join(dataDir, CertificateDir, name), content)
}

// ReadManifest reads a manifest by its path relative to dataDir. It returns
// domain.ErrNotFound when the file is not there, and refuses paths outside the manifests
// directory.
func ReadManifest(dataDir, relative string) ([]byte, error) {
	if relative == "" || !filepath.IsLocal(relative) || filepath.Dir(relative) != ManifestDir {
		return nil, fmt.Errorf("%w: manifest is not written yet", domain.ErrNotFound)
	}
	content, err := os.ReadFile(filepath.Join(dataDir, relative))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: manifest file %s is missing", domain.ErrNotFound, relative)
	}
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", relative, err)
	}
	return content, nil
}

// writeAtomic writes through a temporary file, so a reader on the shared volume never sees
// half a document, and two writers of the same document do not mix their output.
func writeAtomic(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory %s (is the data volume writable?): %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file in %s: %w", dir, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return fmt.Errorf("set mode of %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("move %s into place: %w", path, err)
	}
	return nil
}
