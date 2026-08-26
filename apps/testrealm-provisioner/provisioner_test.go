package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func validSpec() ProvisioningSpec {
	return ProvisioningSpec{Summary: "Test a quest", Complete: true, Characters: []CharacterRequest{{Name: "Tester", Race: "human", Class: "mage", Level: 40, MoneyGold: 100, Items: []ItemRequest{{ID: 6948, Count: 1}}, ActiveQuests: []uint32{1718}}}}
}

func TestValidateSpecRejectsUnsafeValues(t *testing.T) {
	if err := validateSpec(validSpec()); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
	if err := validateSpec(ProvisioningSpec{Summary: "Realm only", Complete: true, RealmOnly: true}); err != nil {
		t.Fatalf("realm-only spec rejected: %v", err)
	}
	tests := []ProvisioningSpec{validSpec(), validSpec(), validSpec(), validSpec()}
	tests[0].Characters[0].Level = 81
	tests[1].Characters[0].MoneyGold = 10001
	tests[2].Characters[0].Name = "x; DROP TABLE"
	tests[3].Characters[0].Class = "shell"
	for i, spec := range tests {
		if err := validateSpec(spec); err == nil {
			t.Errorf("unsafe spec %d was accepted", i)
		}
	}
}

func TestValidateSpecRejectsDuplicateAndControlInputs(t *testing.T) {
	spec := validSpec()
	spec.Characters[0].Items = []ItemRequest{{ID: 1, Count: 1}, {ID: 1, Count: 2}}
	if validateSpec(spec) == nil {
		t.Fatal("duplicate items accepted")
	}
	spec = validSpec()
	spec.Characters[0].ActiveQuests = []uint32{42}
	spec.Characters[0].CompletedQuests = []uint32{42}
	if validateSpec(spec) == nil {
		t.Fatal("duplicate quests accepted")
	}
	spec = validSpec()
	spec.Summary = "hello\nworld"
	if validateSpec(spec) == nil {
		t.Fatal("control characters accepted")
	}
}

func TestImageCatalogRequiresExactDigestAndCommit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "images.json")
	sha := strings.Repeat("a", 40)
	digest := strings.Repeat("b", 64)
	if err := os.WriteFile(path, []byte(`{"42":{"sha":"`+sha+`","image":"ghcr.io/ac/gameserver@sha256:`+digest+`","importer_image":"ghcr.io/ac/dbimport@sha256:`+digest+`"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	c := ImageCatalog{Path: path, AllowedPrefix: "ghcr.io/ac/gameserver", AllowedImporterPrefix: "ghcr.io/ac/dbimport"}
	image, err := c.Resolve(PullRequest{Number: 42, HeadSHA: sha})
	if err != nil || !strings.Contains(image.Image, digest) {
		t.Fatalf("valid image rejected: %v", err)
	}
	if _, err := c.Resolve(PullRequest{Number: 42, HeadSHA: "different"}); err == nil {
		t.Fatal("commit mismatch accepted")
	}
	if err := os.WriteFile(path, []byte(`{"42":{"sha":"`+sha+`","image":"ghcr.io/ac/gameserver.evil@sha256:`+digest+`","importer_image":"ghcr.io/ac/dbimport@sha256:`+digest+`"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Resolve(PullRequest{Number: 42, HeadSHA: sha}); err == nil {
		t.Fatal("lookalike registry path accepted")
	}
}

type recordingExecutor struct{ suspended, deleted, resumed int }

func (r *recordingExecutor) Provision(context.Context, Realm, func(ProvisionProgress)) (ProvisionResult, error) {
	return ProvisionResult{RealmName: "Test", Address: "127.0.0.1", Port: 8085, RealmListID: 2}, nil
}

type concurrentExecutor struct {
	mu           sync.Mutex
	active, peak int
	release      chan struct{}
}

func (e *concurrentExecutor) Provision(_ context.Context, realm Realm, _ func(ProvisionProgress)) (ProvisionResult, error) {
	e.mu.Lock()
	e.active++
	if e.active > e.peak {
		e.peak = e.active
	}
	e.mu.Unlock()
	<-e.release
	e.mu.Lock()
	e.active--
	e.mu.Unlock()
	return ProvisionResult{RealmName: "Test", RealmListID: realm.RealmListID}, nil
}
func (*concurrentExecutor) Suspend(context.Context, Realm) error { return nil }
func (*concurrentExecutor) InspectActivity(context.Context, Realm) (RealmActivity, error) {
	return RealmActivity{}, nil
}
func (*concurrentExecutor) Resume(context.Context, Realm) error { return nil }
func (*concurrentExecutor) Delete(context.Context, Realm) error { return nil }

func TestTwoSlotsProvisionConcurrently(t *testing.T) {
	store, _ := newStore(filepath.Join(t.TempDir(), "state.json"))
	exec := &concurrentExecutor{release: make(chan struct{})}
	s := &Server{store: store, executor: exec}
	for i, realmID := range []uint32{2, 3} {
		_ = store.putRealm(Realm{ID: string(rune('a' + i)), RealmListID: realmID, State: RealmProvisioning})
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); s.provisionRealm("a") }()
	go func() { defer wg.Done(); s.provisionRealm("b") }()
	time.Sleep(50 * time.Millisecond)
	exec.mu.Lock()
	peak := exec.peak
	exec.mu.Unlock()
	close(exec.release)
	wg.Wait()
	if peak != 2 {
		t.Fatalf("expected two concurrent provisions, peak=%d", peak)
	}
}

func TestBuildCoordinatorReportsConfiguredCapacity(t *testing.T) {
	b := &BuildClient{token: "backend-only", maxActive: 2, pending: map[int]time.Time{1: time.Now(), 2: time.Now()}}
	if _, err := b.Ensure(context.Background(), PullRequest{Number: 3}); err == nil || !strings.Contains(err.Error(), "all 2 build slots") {
		t.Fatalf("third concurrent build was not rejected: %v", err)
	}
}

func TestRunningCapacityDoesNotDisableBuildRequests(t *testing.T) {
	store, err := newStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := store.putRealm(Realm{ID: string(rune('a' + i)), State: RealmRunning}); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{store: store, provisioningAvailable: true, maxRunning: 5, builds: &BuildClient{token: "enabled"}}
	recorder := httptest.NewRecorder()
	s.status(recorder, httptest.NewRequest("GET", "/api/status", nil))
	var status struct {
		Available               bool `json:"available"`
		OnlineCapacityAvailable bool `json:"online_capacity_available"`
		RunningRealms           int  `json:"running_realms"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Available || status.OnlineCapacityAvailable || status.RunningRealms != 5 {
		t.Fatalf("build service should remain available while online capacity is full: %#v", status)
	}
}
func (r *recordingExecutor) InspectActivity(context.Context, Realm) (RealmActivity, error) {
	return RealmActivity{}, nil
}
func (r *recordingExecutor) Suspend(context.Context, Realm) error { r.suspended++; return nil }
func (r *recordingExecutor) Resume(context.Context, Realm) error  { r.resumed++; return nil }
func (r *recordingExecutor) Delete(context.Context, Realm) error  { r.deleted++; return nil }

func TestLifecycleSuspendsAndPurges(t *testing.T) {
	store, err := newStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	exec := &recordingExecutor{}
	s := &Server{store: store, executor: exec, idleAfter: 30 * time.Minute, purgeAfter: 48 * time.Hour}
	realm := Realm{ID: "one", State: RealmRunning, LastPlayerSeen: time.Now().Add(-31 * time.Minute), TokenHash: tokenDigest("secret")}
	if err := store.putRealm(realm); err != nil {
		t.Fatal(err)
	}
	s.reconcile(context.Background())
	got, _ := store.realm("one")
	if got.State != RealmOffline || exec.suspended != 1 {
		t.Fatalf("realm was not suspended: %#v", got)
	}
	old := time.Now().Add(-49 * time.Hour)
	got.OfflineAt = &old
	if err := store.putRealm(got); err != nil {
		t.Fatal(err)
	}
	s.reconcile(context.Background())
	got, _ = store.realm("one")
	if got.State != RealmDeleted || got.TokenHash != "" || exec.deleted != 1 {
		t.Fatalf("realm was not purged: %#v", got)
	}
}

type activityExecutor struct {
	recordingExecutor
	activity RealmActivity
	err      error
}

func (e *activityExecutor) InspectActivity(context.Context, Realm) (RealmActivity, error) {
	return e.activity, e.err
}

func TestLifecycleKeepsRealmRunningWhilePlayerConnected(t *testing.T) {
	store, err := newStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	exec := &activityExecutor{activity: RealmActivity{ActiveConnections: 1}}
	s := &Server{store: store, executor: exec, idleAfter: 30 * time.Minute, purgeAfter: 48 * time.Hour}
	old := time.Now().Add(-31 * time.Minute)
	if err := store.putRealm(Realm{ID: "one", State: RealmRunning, LastPlayerSeen: old}); err != nil {
		t.Fatal(err)
	}
	s.reconcile(context.Background())
	got, _ := store.realm("one")
	if got.State != RealmRunning || !got.LastPlayerSeen.After(old) || exec.suspended != 0 {
		t.Fatalf("active realm was not preserved: %#v", got)
	}
}

func TestLifecycleFailsOpenWhenActivityCannotBeInspected(t *testing.T) {
	store, err := newStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	exec := &activityExecutor{err: errors.New("metrics unavailable")}
	s := &Server{store: store, executor: exec, idleAfter: 30 * time.Minute, purgeAfter: 48 * time.Hour}
	if err := store.putRealm(Realm{ID: "one", State: RealmRunning, LastPlayerSeen: time.Now().Add(-31 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	s.reconcile(context.Background())
	got, _ := store.realm("one")
	if got.State != RealmRunning || exec.suspended != 0 {
		t.Fatalf("realm was suspended after monitoring failure: %#v", got)
	}
}

func TestStorePersistsOnlyTokenDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := newStore(path)
	if err != nil {
		t.Fatal(err)
	}
	plain := "management-secret"
	if err := store.putRealm(Realm{ID: "r", TokenHash: tokenDigest(plain)}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), plain) {
		t.Fatal("plaintext management token was persisted")
	}
}

func TestStoreGenerationPersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	first, err := newStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.putSession(ChatSession{ID: "session"}); err != nil {
		t.Fatal(err)
	}
	second, err := newStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if first.generation() == "" || second.generation() != first.generation() {
		t.Fatalf("state generation was not persisted: first=%q second=%q", first.generation(), second.generation())
	}
}
