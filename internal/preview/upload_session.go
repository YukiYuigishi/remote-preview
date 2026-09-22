package preview

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"time"
)

type uploadSession struct {
	id           string
	directory    string
	relative     string
	total        int64
	offset       int64
	file         *os.File
	committing   bool
	lastActivity time.Time
}

type uploadSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*uploadSession
}

const uploadSessionTTL = 24 * time.Hour

func newUploadSessionStore() *uploadSessionStore {
	return &uploadSessionStore{sessions: make(map[string]*uploadSession)}
}

func (s *uploadSessionStore) create(directory, relative string, total int64) (*uploadSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createLocked(directory, relative, total)
}

func (s *uploadSessionStore) createLocked(directory, relative string, total int64) (*uploadSession, error) {
	s.cleanupExpiredLocked(time.Now())
	identifierBytes := make([]byte, 18)
	if _, err := cryptorand.Read(identifierBytes); err != nil {
		return nil, fmt.Errorf("generate upload id: %w", err)
	}
	id := hex.EncodeToString(identifierBytes)
	file, err := os.CreateTemp("", ".ykview-upload-session-*")
	if err != nil {
		return nil, fmt.Errorf("create upload session: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, err
	}
	session := &uploadSession{
		id:           id,
		directory:    directory,
		relative:     relative,
		total:        total,
		file:         file,
		lastActivity: time.Now(),
	}
	s.sessions[id] = session
	return session, nil
}

func (s *uploadSessionStore) cleanupExpiredLocked(now time.Time) {
	for id, session := range s.sessions {
		if session.committing || now.Sub(session.lastActivity) < uploadSessionTTL {
			continue
		}
		s.removeLocked(id)
	}
}

func (s *uploadSessionStore) removeLocked(id string) {
	session, ok := s.sessions[id]
	if !ok {
		return
	}
	delete(s.sessions, id)
	_ = session.file.Close()
	_ = os.Remove(session.file.Name())
}

func (s *uploadSessionStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.sessions {
		s.removeLocked(id)
	}
}
