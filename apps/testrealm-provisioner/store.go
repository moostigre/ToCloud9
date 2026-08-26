package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type persistedState struct {
	Generation string                 `json:"generation"`
	Sessions   map[string]ChatSession `json:"sessions"`
	Realms     map[string]Realm       `json:"realms"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	data persistedState
}

func newStore(path string) (*Store, error) {
	s := &Store{path: path, data: persistedState{Generation: randomID(16), Sessions: map[string]ChatSession{}, Realms: map[string]Realm{}}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, err
	}
	if s.data.Sessions == nil {
		s.data.Sessions = map[string]ChatSession{}
	}
	if s.data.Realms == nil {
		s.data.Realms = map[string]Realm{}
	}
	if s.data.Generation == "" {
		s.data.Generation = randomID(16)
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) generation() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Generation
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Store) runningCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, realm := range s.data.Realms {
		if realm.State == RealmRunning || realm.State == RealmProvisioning || realm.State == RealmDeleting {
			count++
		}
	}
	return count
}

// markUnbackedRealmsSimulated repairs state written by older demo builds which
// incorrectly labelled no-op provisions as playable realms.
func (s *Store) markUnbackedRealmsSimulated() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for id, realm := range s.data.Realms {
		if realm.State != RealmRunning && realm.State != RealmProvisioning {
			continue
		}
		realm.State = RealmSimulated
		realm.Simulated = true
		realm.Progress = "Simulation complete — no playable realm was created"
		realm.RealmName, realm.Address, realm.Port, realm.RealmListID = "", "", 0, 0
		s.data.Realms[id] = realm
		changed = true
	}
	if !changed {
		return nil
	}
	return s.saveLocked()
}

func (s *Store) putSession(session ChatSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Sessions[session.ID] = session
	return s.saveLocked()
}

func (s *Store) session(id string) (ChatSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data.Sessions[id]
	return v, ok
}

func (s *Store) putRealm(realm Realm) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Realms[realm.ID] = realm
	return s.saveLocked()
}

func (s *Store) realm(id string) (Realm, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data.Realms[id]
	return v, ok
}

func (s *Store) realms() []Realm {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Realm, 0, len(s.data.Realms))
	for _, realm := range s.data.Realms {
		out = append(out, realm)
	}
	return out
}
