package main

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var hexID = regexp.MustCompile(`^[a-f0-9]{24}$`)
var sha = regexp.MustCompile(`^[a-f0-9]{40}$`)
var digest = regexp.MustCompile(`^ghcr\.io/moostigre/tc9-ac-pr(?:-dbimport)?@sha256:[a-f0-9]{64}$`)

type item struct {
	ID    uint32 `json:"id"`
	Count uint16 `json:"count"`
}
type character struct {
	Name      string   `json:"name"`
	Race      string   `json:"race"`
	Class     string   `json:"class"`
	Level     uint8    `json:"level"`
	Money     uint32   `json:"money_gold"`
	Items     []item   `json:"items"`
	Active    []uint32 `json:"active_quests"`
	Completed []uint32 `json:"completed_quests"`
}
type fixture struct {
	Summary    string      `json:"summary"`
	Complete   bool        `json:"complete"`
	RealmOnly  bool        `json:"realm_only"`
	Question   string      `json:"question"`
	Characters []character `json:"characters"`
}
type command struct {
	Action        string  `json:"action"`
	RealmID       string  `json:"realm_id"`
	Namespace     string  `json:"namespace"`
	CommitSHA     string  `json:"commit_sha"`
	Image         string  `json:"image"`
	ImporterImage string  `json:"importer_image"`
	AdminUser     string  `json:"admin_user"`
	AdminPass     string  `json:"admin_password"`
	PurgeImage    bool    `json:"purge_image"`
	RealmListID   uint32  `json:"realmlist_id"`
	PRNumber      int     `json:"pr_number"`
	Fixture       fixture `json:"fixture"`
}

type server struct {
	secret []byte
	script string
}

func main() {
	secret := os.Getenv("OPERATOR_HMAC_SECRET")
	if len(secret) < 32 {
		slog.Error("OPERATOR_HMAC_SECRET is invalid")
		os.Exit(1)
	}
	s := &server{secret: []byte(secret), script: env("OPERATOR_SCRIPT", "/usr/local/libexec/tc9-realm")}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/realm-commands", s.handle)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	h := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 12 * time.Minute, MaxHeaderBytes: 16 << 10}
	slog.Info("realm operator listening")
	if err := h.ListenAndServe(); err != nil {
		slog.Error("operator stopped", "error", err)
		os.Exit(1)
	}
}

func (s *server) handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		problem(w, 400, "invalid request")
		return
	}
	if !s.authorized(r, body) {
		problem(w, 401, "invalid signature")
		return
	}
	var c command
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		slog.Warn("invalid command JSON", "error", err)
		problem(w, 400, "invalid realm command")
		return
	}
	if !valid(c) {
		slog.Warn("invalid command fields", "action", c.Action, "realm", c.RealmID, "namespace", c.Namespace, "realmlist_id", c.RealmListID, "pr", c.PRNumber)
		problem(w, 400, "invalid realm command")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	input, _ := json.Marshal(c.Fixture)
	cmd := exec.CommandContext(ctx, s.script, c.Action, c.RealmID, c.Namespace, strconv.FormatUint(uint64(c.RealmListID), 10), strconv.Itoa(c.PRNumber), c.CommitSHA, c.Image, c.ImporterImage)
	cmd.Env = append(os.Environ(), "TC9_FIXTURE_JSON="+string(input))
	if c.Action == "provision" {
		s.streamProvision(w, cmd, c)
		return
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("realm command failed", "action", c.Action, "realm", c.RealmID, "error", err, "output", truncate(string(output), 4000))
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 3 {
			problem(w, http.StatusUnprocessableEntity, "character fixtures are not available yet")
			return
		}
		problem(w, 503, "realm operation failed")
		return
	}
	if c.Action == "inspect" {
		w.Header().Set("Content-Type", "application/json")
		w.Write(output)
		return
	}
	if c.Action != "provision" {
		w.WriteHeader(204)
		return
	}
	result := map[string]any{"realm_name": fmt.Sprintf("PR %d Test", c.PRNumber), "address": env("PUBLIC_ADDRESS", "163.172.51.144"), "port": 32758 + c.RealmListID, "realmlist_id": c.RealmListID}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *server) streamProvision(w http.ResponseWriter, cmd *exec.Cmd, c command) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		problem(w, 500, "realm operation failed")
		return
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		problem(w, 503, "realm operation failed")
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	enc := json.NewEncoder(w)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 256<<10)
	var captured strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "TC9_PROGRESS\t") {
			parts := strings.SplitN(line, "\t", 4)
			pct, parseErr := 0, fmt.Errorf("invalid progress event")
			if len(parts) == 4 {
				pct, parseErr = strconv.Atoi(parts[2])
			}
			if parseErr == nil && pct >= -1 && pct <= 100 {
				_ = enc.Encode(map[string]any{"type": "progress", "phase": parts[1], "percent": pct, "detail": truncate(parts[3], 240)})
				if flusher != nil {
					flusher.Flush()
				}
				continue
			}
		}
		if captured.Len() < 4000 {
			captured.WriteString(line + "\n")
		}
	}
	err = cmd.Wait()
	if err != nil {
		output := captured.String() + stderr.String()
		slog.Error("realm command failed", "action", c.Action, "realm", c.RealmID, "error", err, "output", truncate(output, 4000))
		code := "realm_operation_failed"
		message := "Realm operation failed"
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 3 {
			code, message = "character_fixtures_unavailable", "Character fixtures are not available yet"
		}
		_ = enc.Encode(map[string]any{"type": "error", "code": code, "error": message})
		return
	}
	result := map[string]any{"realm_name": fmt.Sprintf("PR %d Test", c.PRNumber), "address": env("PUBLIC_ADDRESS", "163.172.51.144"), "port": 32758 + c.RealmListID, "realmlist_id": c.RealmListID}
	_ = enc.Encode(map[string]any{"type": "result", "result": result})
}

func (s *server) authorized(r *http.Request, body []byte) bool {
	ts := r.Header.Get("X-TC9-Timestamp")
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || time.Since(time.Unix(unix, 0)) > 5*time.Minute || time.Until(time.Unix(unix, 0)) > time.Minute {
		return false
	}
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	got, err := hex.DecodeString(r.Header.Get("X-TC9-Signature"))
	return err == nil && hmac.Equal(mac.Sum(nil), got)
}

func valid(c command) bool {
	if !hexID.MatchString(c.RealmID) || !sha.MatchString(c.CommitSHA) || c.PRNumber < 1 {
		return false
	}
	slot := map[string]uint32{"tc9-testrealm-slot-1": 2, "tc9-testrealm-slot-2": 3, "tc9-testrealm-slot-3": 4, "tc9-testrealm-slot-4": 5, "tc9-testrealm-slot-5": 6}
	if slot[c.Namespace] != c.RealmListID {
		return false
	}
	if c.Action != "provision" && c.Action != "inspect" && c.Action != "suspend" && c.Action != "resume" && c.Action != "delete" {
		return false
	}
	if c.Action == "provision" && (c.AdminUser != "admin" || c.AdminPass != "admin" || !digest.MatchString(c.Image) || !digest.MatchString(c.ImporterImage)) {
		return false
	}
	return len(c.Fixture.Characters) <= 1
}

func problem(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func truncate(v string, n int) string {
	if len(v) > n {
		return v[:n]
	}
	return v
}
