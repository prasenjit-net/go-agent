// Package filestore implements agent.ConversationStore backed by one JSON
// file per session on local disk. It exists partly as a convenience for
// CLIs and single-process apps that want history to survive a restart
// without standing up a database, and partly as a reference implementation
// proving agent.ConversationStore's two-method contract is implementable
// outside the root package (agent.NewInMemoryStore, the built-in default,
// is the other).
//
// It is not a production-grade choice for multi-process or networked
// deployments: writes are atomic per file (write-temp-then-rename) but
// there is no cross-process locking, so concurrent writers to the same
// session ID from different processes can race. Single-process concurrent
// use is safe (Session itself documents that concurrent Send calls on the
// same session ID need external synchronization regardless of store).
package filestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	agent "github.com/prasenjit-net/go-agent"
)

// Store implements agent.ConversationStore, persisting each session as one
// JSON file under a directory on local disk.
type Store struct {
	dir string
}

// New returns a Store that persists sessions under dir, creating it
// (including any missing parents) if it doesn't already exist.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("filestore: creating %s: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

// Load implements agent.ConversationStore. A session with no stored file
// yet returns (nil, nil), matching agent.NewInMemoryStore's behavior for an
// unknown session ID.
func (s *Store) Load(_ context.Context, sessionID string) ([]agent.Message, error) {
	data, err := os.ReadFile(s.path(sessionID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("filestore: loading %s: %w", sessionID, err)
	}

	var wms []wireMessage
	if err := json.Unmarshal(data, &wms); err != nil {
		return nil, fmt.Errorf("filestore: decoding %s: %w", sessionID, err)
	}
	msgs, err := fromWireMessages(wms)
	if err != nil {
		return nil, fmt.Errorf("filestore: decoding %s: %w", sessionID, err)
	}
	return msgs, nil
}

// Save implements agent.ConversationStore. It writes to a temporary file in
// the same directory and renames it into place, so a crash or concurrent
// Load mid-write never observes a truncated file.
func (s *Store) Save(_ context.Context, sessionID string, msgs []agent.Message) error {
	wms, err := toWireMessages(msgs)
	if err != nil {
		return fmt.Errorf("filestore: encoding %s: %w", sessionID, err)
	}
	data, err := json.Marshal(wms)
	if err != nil {
		return fmt.Errorf("filestore: encoding %s: %w", sessionID, err)
	}

	target := s.path(sessionID)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("filestore: writing %s: %w", sessionID, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("filestore: saving %s: %w", sessionID, err)
	}
	return nil
}

// path returns the on-disk path for sessionID, escaping it so a session ID
// containing "/" or ".." can't write outside dir.
func (s *Store) path(sessionID string) string {
	return filepath.Join(s.dir, url.PathEscape(sessionID)+".json")
}

var _ agent.ConversationStore = (*Store)(nil)
