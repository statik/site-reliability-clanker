package forge

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/memory"
)

// cacheLimit is the number of git objects that one repository keeps in memory. An object
// never changes, so a cached object is always correct.
const cacheLimit = 20000

// sqlStorage keeps the objects and the references of one repository in the database of the
// control plane. The other parts of a git storage (configuration, index, shallow commits)
// are not needed by a bare repository that only this process uses, so they stay in memory.
//
// The database pool has one connection. No method here holds a result set open while it
// runs a second query.
type sqlStorage struct {
	*memory.Storage
	db   *sql.DB
	repo string

	mu    sync.Mutex
	cache map[plumbing.Hash]*plumbing.MemoryObject
}

func newStorage(db *sql.DB, repo string) *sqlStorage {
	return &sqlStorage{Storage: memory.NewStorage(), db: db, repo: repo,
		cache: map[plumbing.Hash]*plumbing.MemoryObject{}}
}

func (s *sqlStorage) remember(hash plumbing.Hash, object *plumbing.MemoryObject) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cache) >= cacheLimit {
		s.cache = map[plumbing.Hash]*plumbing.MemoryObject{}
	}
	s.cache[hash] = object
}

func (s *sqlStorage) cached(hash plumbing.Hash) *plumbing.MemoryObject {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache[hash]
}

func memoryObject(kind plumbing.ObjectType, content []byte) *plumbing.MemoryObject {
	object := &plumbing.MemoryObject{}
	object.SetType(kind)
	// A write to a memory object cannot fail.
	_, _ = object.Write(content)
	return object
}

// NewEncodedObject returns an empty object that is not stored yet.
func (s *sqlStorage) NewEncodedObject() plumbing.EncodedObject { return &plumbing.MemoryObject{} }

// SetEncodedObject stores the object. An object that is stored already is left as it is.
func (s *sqlStorage) SetEncodedObject(object plumbing.EncodedObject) (plumbing.Hash, error) {
	hash := object.Hash()
	if s.cached(hash) != nil {
		return hash, nil
	}
	reader, err := object.Reader()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("read git object %s: %w", hash, err)
	}
	content, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("read git object %s: %w", hash, err)
	}
	if _, err := s.db.Exec(`INSERT INTO git_objects (repo, hash, kind, content)
		VALUES ($1,$2,$3,$4) ON CONFLICT (repo, hash) DO NOTHING`, s.repo, hash.String(),
		int(object.Type()), content); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store git object %s of %s: %w", hash, s.repo, err)
	}
	s.remember(hash, memoryObject(object.Type(), content))
	return hash, nil
}

func (s *sqlStorage) load(hash plumbing.Hash) (*plumbing.MemoryObject, error) {
	if object := s.cached(hash); object != nil {
		return object, nil
	}
	var kind int
	var content []byte
	err := s.db.QueryRow(`SELECT kind, content FROM git_objects WHERE repo = $1 AND hash = $2`,
		s.repo, hash.String()).Scan(&kind, &content)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, plumbing.ErrObjectNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load git object %s of %s: %w", hash, s.repo, err)
	}
	object := memoryObject(plumbing.ObjectType(kind), content)
	s.remember(hash, object)
	return object, nil
}

// EncodedObject returns the object with that hash. kind can be plumbing.AnyObject.
func (s *sqlStorage) EncodedObject(kind plumbing.ObjectType, hash plumbing.Hash) (plumbing.EncodedObject, error) {
	object, err := s.load(hash)
	if err != nil {
		return nil, err
	}
	if kind != plumbing.AnyObject && object.Type() != kind {
		return nil, plumbing.ErrObjectNotFound
	}
	return object, nil
}

// HasEncodedObject returns nil when the object is stored.
func (s *sqlStorage) HasEncodedObject(hash plumbing.Hash) error {
	_, err := s.load(hash)
	return err
}

// EncodedObjectSize returns the size of the content of the object.
func (s *sqlStorage) EncodedObjectSize(hash plumbing.Hash) (int64, error) {
	object, err := s.load(hash)
	if err != nil {
		return 0, err
	}
	return object.Size(), nil
}

// IterEncodedObjects returns every object of that kind. It reads them all before it
// returns, because the one database connection must be free for the caller.
func (s *sqlStorage) IterEncodedObjects(kind plumbing.ObjectType) (storer.EncodedObjectIter, error) {
	query, args := `SELECT kind, content FROM git_objects WHERE repo = $1`, []any{s.repo}
	if kind != plumbing.AnyObject {
		query += ` AND kind = $2`
		args = append(args, int(kind))
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list git objects of %s: %w", s.repo, err)
	}
	defer rows.Close()
	objects := []plumbing.EncodedObject{}
	for rows.Next() {
		var rowKind int
		var content []byte
		if err := rows.Scan(&rowKind, &content); err != nil {
			return nil, fmt.Errorf("read git object of %s: %w", s.repo, err)
		}
		objects = append(objects, memoryObject(plumbing.ObjectType(rowKind), content))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list git objects of %s: %w", s.repo, err)
	}
	return storer.NewEncodedObjectSliceIter(objects), nil
}

// AddAlternate is not supported: a repository in the database has no alternates.
func (s *sqlStorage) AddAlternate(string) error {
	return errors.New("a repository in the database has no alternates")
}

func referenceRow(reference *plumbing.Reference) (name, target string) {
	parts := reference.Strings()
	return parts[0], parts[1]
}

// SetReference writes the reference.
func (s *sqlStorage) SetReference(reference *plumbing.Reference) error {
	name, target := referenceRow(reference)
	_, err := s.db.Exec(`INSERT INTO git_refs (repo, name, target) VALUES ($1,$2,$3)
		ON CONFLICT (repo, name) DO UPDATE SET target = excluded.target`, s.repo, name, target)
	if err != nil {
		return fmt.Errorf("set reference %s of %s: %w", name, s.repo, err)
	}
	return nil
}

// CheckAndSetReference writes the reference only if it still has the old value, so that
// two writers cannot both move one branch.
func (s *sqlStorage) CheckAndSetReference(updated, old *plumbing.Reference) error {
	if old == nil {
		return s.SetReference(updated)
	}
	name, target := referenceRow(updated)
	_, oldTarget := referenceRow(old)
	result, err := s.db.Exec(`UPDATE git_refs SET target = $1
		WHERE repo = $2 AND name = $3 AND target = $4`, target, s.repo, name, oldTarget)
	if err != nil {
		return fmt.Errorf("move reference %s of %s: %w", name, s.repo, err)
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return storage.ErrReferenceHasChanged
	}
	return nil
}

// Reference returns the reference with that name.
func (s *sqlStorage) Reference(name plumbing.ReferenceName) (*plumbing.Reference, error) {
	var target string
	err := s.db.QueryRow(`SELECT target FROM git_refs WHERE repo = $1 AND name = $2`, s.repo,
		name.String()).Scan(&target)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, plumbing.ErrReferenceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read reference %s of %s: %w", name, s.repo, err)
	}
	return plumbing.NewReferenceFromStrings(name.String(), target), nil
}

// IterReferences returns every reference of the repository.
func (s *sqlStorage) IterReferences() (storer.ReferenceIter, error) {
	rows, err := s.db.Query(`SELECT name, target FROM git_refs WHERE repo = $1 ORDER BY name`,
		s.repo)
	if err != nil {
		return nil, fmt.Errorf("list references of %s: %w", s.repo, err)
	}
	defer rows.Close()
	references := []*plumbing.Reference{}
	for rows.Next() {
		var name, target string
		if err := rows.Scan(&name, &target); err != nil {
			return nil, fmt.Errorf("read reference of %s: %w", s.repo, err)
		}
		references = append(references, plumbing.NewReferenceFromStrings(name, target))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list references of %s: %w", s.repo, err)
	}
	return storer.NewReferenceSliceIter(references), nil
}

// RemoveReference deletes the reference.
func (s *sqlStorage) RemoveReference(name plumbing.ReferenceName) error {
	if _, err := s.db.Exec(`DELETE FROM git_refs WHERE repo = $1 AND name = $2`, s.repo,
		name.String()); err != nil {
		return fmt.Errorf("remove reference %s of %s: %w", name, s.repo, err)
	}
	return nil
}

// CountLooseRefs returns 0: the database has no loose or packed references.
func (s *sqlStorage) CountLooseRefs() (int, error) { return 0, nil }

// PackRefs does nothing: the database has no loose or packed references.
func (s *sqlStorage) PackRefs() error { return nil }
