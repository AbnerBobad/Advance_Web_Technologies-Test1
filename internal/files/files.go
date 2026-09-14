package files

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Store writes uploaded originals and later variant bytes to the local
// filesystem using server-controlled names. Submitted filenames are never
// used as filesystem paths.
type Store struct {
	dir string
}

// NewStore creates the storage directory if needed and returns a Store that
// writes below it.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Save durably writes the given bytes and returns the server-controlled
// stored filename. ext is derived by the caller from the sniffed media type.
func (s *Store) Save(data []byte, ext string) (string, error) {
	name, err := uniqueName(ext)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(s.dir, name), data, 0o600); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return name, nil
}

// Read returns the bytes of a previously stored file. The worker uses it to
// read the original before generating variants, and the variant endpoint uses
// it to serve the generated files.
func (s *Store) Read(name string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, name))
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	return data, nil
}

// Remove deletes a stored file, used to clean up the input file when the
// durable acceptance record cannot be created.
func (s *Store) Remove(name string) error {
	return os.Remove(filepath.Join(s.dir, name))
}

func uniqueName(ext string) (string, error) {
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return fmt.Sprintf("img_%d_%s%s", time.Now().UnixNano(), hex.EncodeToString(rnd[:]), ext), nil
}
