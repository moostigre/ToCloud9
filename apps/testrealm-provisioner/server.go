package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Server struct {
	store                 *Store
	github                *GitHubClient
	builds                *BuildClient
	images                ImageCatalog
	executor              RealmExecutor
	provisioningAvailable bool
	maintenanceMessage    string
	activitySecret        []byte
	catalogueSecret       []byte
	maxRunning            int
	idleAfter, purgeAfter time.Duration
	opMu                  sync.Mutex
	slotMu                [5]sync.Mutex
	rateMu                sync.Mutex
	rates                 map[string]*rateBucket
}

type rateBucket struct {
	Start time.Time
	Count int
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.index)
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/prs", s.listPRs)
	mux.HandleFunc("GET /api/overview", s.overview)
	mux.HandleFunc("POST /api/sessions", s.createSession)
	mux.HandleFunc("POST /api/sessions/{id}/realms", s.createRealm)
	mux.HandleFunc("GET /api/realms/{id}", s.getRealm)
	mux.HandleFunc("POST /api/realms/{id}/reactivate", s.reactivateRealm)
	mux.HandleFunc("DELETE /api/realms/{id}", s.deleteRealm)
	mux.HandleFunc("POST /internal/realms/{id}/activity", s.playerActivity)
	mux.HandleFunc("POST /internal/catalogue", s.updateCatalogue)
	return s.middleware(mux)
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("Cache-Control", "no-store")
		if r.ContentLength > 64<<10 {
			problem(w, http.StatusRequestEntityTooLarge, "request too large")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet {
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				problem(w, http.StatusUnsupportedMediaType, "application/json is required")
				return
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				problem(w, http.StatusForbidden, "cross-site requests are not allowed")
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
				problem(w, http.StatusForbidden, "invalid request origin")
				return
			}
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet && !s.allow(r) {
			problem(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) allow(r *http.Request) bool {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if host == "" {
		host = r.RemoteAddr
	}
	now := time.Now()
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	b := s.rates[host]
	if b == nil || now.Sub(b.Start) >= time.Minute {
		s.rates[host] = &rateBucket{Start: now, Count: 1}
		return true
	}
	b.Count++
	return b.Count <= 30
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	imagesAvailable := s.images.Available()
	buildsAvailable := s.builds != nil && s.builds.enabled()
	running := s.store.runningCount()
	jsonResponse(w, http.StatusOK, map[string]any{"available": s.maintenanceMessage == "" && s.provisioningAvailable && (imagesAvailable || buildsAvailable), "online_capacity_available": running < s.maxRunning, "maintenance_message": s.maintenanceMessage, "operator_available": s.provisioningAvailable, "images_available": imagesAvailable, "builds_available": buildsAvailable, "running_realms": running, "max_running_realms": s.maxRunning, "state_generation": s.store.generation()})
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	realms := make([]map[string]any, 0, 2)
	for _, realm := range s.store.realms() {
		if realm.State != RealmProvisioning && realm.State != RealmRunning && realm.State != RealmOffline && realm.State != RealmDeleting {
			continue
		}
		realms = append(realms, map[string]any{"id": realm.ID, "realmlist_id": realm.RealmListID, "pr_number": realm.PRNumber, "state": realm.State, "realm_name": realm.RealmName})
	}
	sort.Slice(realms, func(i, j int) bool { return realms[i]["realmlist_id"].(uint32) < realms[j]["realmlist_id"].(uint32) })
	builds, err := s.builds.Active(r.Context())
	if err != nil {
		builds = []Build{}
	}
	jsonResponse(w, http.StatusOK, map[string]any{"realms": realms, "builds": builds, "max_concurrent_builds": s.builds.maxActive, "build_status_available": err == nil && s.builds.enabled()})
}

func (s *Server) listPRs(w http.ResponseWriter, r *http.Request) {
	prs, err := s.github.OpenPRs(r.Context())
	if err != nil {
		slog.Warn("PR catalogue request failed", "error", err)
		problem(w, http.StatusServiceUnavailable, "PR catalogue unavailable")
		return
	}
	for i := range prs {
		prs[i].Body = ""
	}
	jsonResponse(w, http.StatusOK, prs)
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PRNumber int `json:"pr_number"`
	}
	if !decodeJSON(w, r, &in) || in.PRNumber < 1 {
		return
	}
	pr, err := s.github.OpenPR(r.Context(), in.PRNumber)
	if err != nil {
		problem(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC()
	session := ChatSession{ID: randomID(18), PR: pr, CreatedAt: now, ExpiresAt: now.Add(6 * time.Hour)}
	if err := s.store.putSession(session); err != nil {
		problem(w, 500, "could not persist session")
		return
	}
	jsonResponse(w, http.StatusCreated, session)
}

func (s *Server) createRealm(w http.ResponseWriter, r *http.Request) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.maintenanceMessage != "" {
		problem(w, 503, s.maintenanceMessage)
		return
	}
	if !s.provisioningAvailable {
		problem(w, 503, "realm provisioning is temporarily unavailable")
		return
	}
	session, ok := s.store.session(r.PathValue("id"))
	if !ok || time.Now().After(session.ExpiresAt) {
		problem(w, 404, "session not found or expired")
		return
	}
	var in struct {
		Spec ProvisioningSpec `json:"spec"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	session.Draft = in.Spec
	if err := validateSpec(session.Draft); err != nil {
		problem(w, 400, err.Error())
		return
	}
	pr, err := s.github.OpenPR(r.Context(), session.PR.Number)
	if err != nil || pr.HeadSHA != session.PR.HeadSHA {
		problem(w, 409, "PR changed or is no longer eligible")
		return
	}
	images, err := s.images.Resolve(pr)
	if err != nil {
		build, buildErr := s.builds.Ensure(r.Context(), pr)
		if buildErr != nil {
			problem(w, 503, buildErr.Error())
			return
		}
		jsonResponse(w, http.StatusAccepted, map[string]any{"build": build, "message": "PR image build queued or in progress"})
		return
	}
	if s.store.runningCount() >= s.maxRunning {
		jsonResponse(w, http.StatusAccepted, map[string]any{"waiting_for_capacity": true, "message": "PR images are ready; waiting for an online realm slot", "build": Build{PRNumber: pr.Number, Status: "complete", Progress: 100, Detail: "PR images are ready; waiting for an online realm slot", UpdatedAt: time.Now().UTC()}})
		return
	}
	token := randomID(32)
	now := time.Now().UTC()
	id := randomID(12)
	namespace, realmListID, ok := s.freeSlot()
	if !ok {
		jsonResponse(w, http.StatusAccepted, map[string]any{"waiting_for_capacity": true, "message": "PR images are ready; waiting for a retained realm slot to be released", "build": Build{PRNumber: pr.Number, Status: "complete", Progress: 100, Detail: "PR images are ready; waiting for a retained realm slot to be released", UpdatedAt: time.Now().UTC()}})
		return
	}
	realm := Realm{ID: id, Namespace: namespace, RealmListID: realmListID, PRNumber: pr.Number, CommitSHA: pr.HeadSHA, Image: images.Image, ImporterImage: images.ImporterImage, State: RealmProvisioning, Spec: session.Draft, TokenHash: tokenDigest(token), CreatedAt: now, LastPlayerSeen: now, Progress: "Queued"}
	if err := s.store.putRealm(realm); err != nil {
		problem(w, 500, "could not persist realm")
		return
	}
	go s.provisionRealm(realm.ID)
	jsonResponse(w, 201, map[string]any{"realm": publicRealm(realm), "management_token": token, "warning": "This token is shown once. Store it securely."})
}

func (s *Server) freeSlot() (string, uint32, bool) {
	used := map[string]bool{}
	for _, realm := range s.store.realms() {
		if realm.State == RealmProvisioning || realm.State == RealmRunning || realm.State == RealmOffline {
			used[realm.Namespace] = true
		}
	}
	for i := 1; i <= len(s.slotMu); i++ {
		ns := "tc9-testrealm-slot-" + strconv.Itoa(i)
		if !used[ns] {
			return ns, uint32(i + 1), true
		}
	}
	return "", 0, false
}

func (s *Server) provisionRealm(id string) {
	realm, ok := s.store.realm(id)
	if !ok || realm.State != RealmProvisioning {
		return
	}
	lock := s.slotLock(realm.RealmListID)
	lock.Lock()
	defer lock.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	result, err := s.executor.Provision(ctx, realm, func(progress ProvisionProgress) {
		now := time.Now().UTC()
		realm.Progress = progress.Detail
		realm.ProgressPhase = progress.Phase
		realm.ProgressPct = progress.Percent
		realm.ProgressDetail = progress.Detail
		realm.ProgressAt = &now
		_ = s.store.putRealm(realm)
	})
	if err != nil {
		realm.State = RealmFailed
		realm.Progress = "Provisioning failed"
		if errors.Is(err, ErrCharacterFixturesUnavailable) {
			realm.Failure = err.Error()
		} else {
			realm.Failure = "realm operator unavailable"
		}
		_ = s.store.putRealm(realm)
		return
	}
	if result.Simulated {
		realm.State = RealmSimulated
		realm.Simulated = true
		realm.Progress = "Simulation complete — no playable realm was created"
		_ = s.store.putRealm(realm)
		return
	}
	realm.RealmName, realm.Address, realm.Port, realm.RealmListID = result.RealmName, result.Address, result.Port, result.RealmListID
	realm.State = RealmRunning
	realm.Progress = "Realm ready and verified in the authentication realm list"
	realm.ProgressPhase, realm.ProgressPct, realm.ProgressDetail = "ready", 100, realm.Progress
	now := time.Now().UTC()
	realm.ProgressAt = &now
	_ = s.store.putRealm(realm)
}

func (s *Server) slotLock(id uint32) *sync.Mutex {
	index := int(id) - 2
	if index < 0 || index >= len(s.slotMu) {
		return &s.opMu
	}
	return &s.slotMu[index]
}

func (s *Server) getRealm(w http.ResponseWriter, r *http.Request) {
	realm, ok := s.authorizedRealm(w, r)
	if !ok {
		return
	}
	jsonResponse(w, 200, publicRealm(realm))
}
func (s *Server) reactivateRealm(w http.ResponseWriter, r *http.Request) {
	realm, ok := s.authorizedRealm(w, r)
	if !ok {
		return
	}
	lock := s.slotLock(realm.RealmListID)
	lock.Lock()
	defer lock.Unlock()
	if realm.State != RealmOffline {
		problem(w, 409, "realm is not offline")
		return
	}
	if s.store.runningCount() >= s.maxRunning {
		problem(w, 503, "realm capacity is temporarily unavailable")
		return
	}
	if err := s.executor.Resume(r.Context(), realm); err != nil {
		problem(w, 503, "realm operator unavailable")
		return
	}
	realm.State = RealmRunning
	realm.OfflineAt = nil
	realm.LastPlayerSeen = time.Now().UTC()
	_ = s.store.putRealm(realm)
	jsonResponse(w, 200, publicRealm(realm))
}
func (s *Server) deleteRealm(w http.ResponseWriter, r *http.Request) {
	realm, ok := s.authorizedRealm(w, r)
	if !ok {
		return
	}
	lock := s.slotLock(realm.RealmListID)
	lock.Lock()
	defer lock.Unlock()
	if realm.State == RealmDeleted {
		w.WriteHeader(204)
		return
	}
	realm.State = RealmDeleting
	realm.Progress = "Deleting realm workloads, databases, and registration"
	realm.ProgressPhase = "deleting"
	now := time.Now().UTC()
	realm.ProgressAt = &now
	_ = s.store.putRealm(realm)
	if err := s.executor.Delete(r.Context(), realm); err != nil {
		problem(w, 503, "realm deletion is incomplete; retry with the same realm ID and token")
		return
	}
	now = time.Now().UTC()
	realm.State = RealmDeleted
	realm.DeletedAt = &now
	realm.TokenHash = ""
	_ = s.store.putRealm(realm)
	w.WriteHeader(204)
}

func (s *Server) authorizedRealm(w http.ResponseWriter, r *http.Request) (Realm, bool) {
	realm, ok := s.store.realm(r.PathValue("id"))
	if !ok {
		problem(w, 404, "realm not found")
		return Realm{}, false
	}
	auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if auth == "" || !hmac.Equal([]byte(tokenDigest(auth)), []byte(realm.TokenHash)) {
		problem(w, 401, "invalid management token")
		return Realm{}, false
	}
	return realm, true
}

func (s *Server) playerActivity(w http.ResponseWriter, r *http.Request) {
	realm, ok := s.store.realm(r.PathValue("id"))
	if !ok {
		problem(w, 404, "realm not found")
		return
	}
	ts := r.Header.Get("X-TC9-Timestamp")
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || time.Since(time.Unix(unix, 0)) > 5*time.Minute || time.Until(time.Unix(unix, 0)) > time.Minute {
		problem(w, 401, "invalid activity timestamp")
		return
	}
	mac := hmac.New(sha256.New, s.activitySecret)
	mac.Write([]byte(ts + "." + realm.ID))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(r.Header.Get("X-TC9-Signature"))) {
		problem(w, 401, "invalid activity signature")
		return
	}
	realm.LastPlayerSeen = time.Now().UTC()
	_ = s.store.putRealm(realm)
	w.WriteHeader(204)
}

func (s *Server) updateCatalogue(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<10))
	if err != nil {
		problem(w, 400, "invalid request")
		return
	}
	ts := r.Header.Get("X-TC9-Timestamp")
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || time.Since(time.Unix(unix, 0)) > 5*time.Minute || time.Until(time.Unix(unix, 0)) > time.Minute {
		problem(w, 401, "invalid catalogue timestamp")
		return
	}
	mac := hmac.New(sha256.New, s.catalogueSecret)
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	if !hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(r.Header.Get("X-TC9-Signature"))) {
		problem(w, 401, "invalid catalogue signature")
		return
	}
	var in struct {
		PRNumber      int    `json:"pr_number"`
		SHA           string `json:"sha"`
		Image         string `json:"image"`
		ImporterImage string `json:"importer_image"`
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || in.PRNumber < 1 {
		problem(w, 400, "invalid catalogue entry")
		return
	}
	pr, err := s.github.OpenPR(r.Context(), in.PRNumber)
	if err != nil {
		problem(w, 409, "PR is no longer eligible")
		return
	}
	if err := s.images.Put(pr, CatalogEntry{SHA: in.SHA, Image: in.Image, ImporterImage: in.ImporterImage}); err != nil {
		problem(w, 400, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) lifecycle(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcile(ctx)
		}
	}
}

func (s *Server) reconcile(ctx context.Context) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	now := time.Now().UTC()
	for _, realm := range s.store.realms() {
		switch {
		case realm.State == RealmRunning:
			activity, err := s.executor.InspectActivity(ctx, realm)
			if err != nil {
				// Monitoring failures must never be interpreted as inactivity.
				continue
			}
			if activity.ActiveConnections > 0 {
				realm.LastPlayerSeen = now
				_ = s.store.putRealm(realm)
				continue
			}
			if now.Sub(realm.LastPlayerSeen) < s.idleAfter {
				continue
			}
			if err := s.executor.Suspend(ctx, realm); err == nil {
				realm.State = RealmOffline
				realm.OfflineAt = &now
				_ = s.store.putRealm(realm)
			}
		case realm.State == RealmOffline && realm.OfflineAt != nil && now.Sub(*realm.OfflineAt) >= s.purgeAfter:
			if err := s.executor.Delete(ctx, realm); err == nil {
				realm.State = RealmDeleted
				realm.DeletedAt = &now
				realm.TokenHash = ""
				_ = s.store.putRealm(realm)
			}
		}
	}
}

func publicRealm(r Realm) map[string]any {
	return map[string]any{"id": r.ID, "pr_number": r.PRNumber, "commit_sha": r.CommitSHA, "image": r.Image, "state": r.State, "progress": r.Progress, "progress_phase": r.ProgressPhase, "progress_percent": r.ProgressPct, "progress_detail": r.ProgressDetail, "progress_updated_at": r.ProgressAt, "failure": r.Failure, "simulated": r.Simulated, "realm_name": r.RealmName, "address": r.Address, "port": r.Port, "realmlist_id": r.RealmListID, "admin_username": "admin", "admin_password": "admin", "created_at": r.CreatedAt, "last_player_seen": r.LastPlayerSeen, "offline_at": r.OfflineAt, "deleted_at": r.DeletedAt, "summary": r.Spec.Summary}
}
func randomID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		problem(w, 400, "invalid JSON request")
		return false
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		problem(w, 400, "request must contain one JSON object")
		return false
	}
	return true
}
func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func problem(w http.ResponseWriter, status int, message string) {
	jsonResponse(w, status, map[string]string{"error": message})
}
