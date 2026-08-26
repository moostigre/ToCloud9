package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type RealmExecutor interface {
	Provision(context.Context, Realm, func(ProvisionProgress)) (ProvisionResult, error)
	InspectActivity(context.Context, Realm) (RealmActivity, error)
	Suspend(context.Context, Realm) error
	Resume(context.Context, Realm) error
	Delete(context.Context, Realm) error
}

type RealmActivity struct {
	ActiveConnections int `json:"active_connections"`
}

type ProvisionProgress struct {
	Phase   string `json:"phase"`
	Percent int    `json:"percent"`
	Detail  string `json:"detail"`
}

type ProvisionResult struct {
	RealmName   string `json:"realm_name"`
	Address     string `json:"address"`
	Port        uint16 `json:"port"`
	RealmListID uint32 `json:"realmlist_id"`
	Simulated   bool   `json:"simulated,omitempty"`
}

var ErrCharacterFixturesUnavailable = errors.New("character fixtures are not available yet; select Realm only")

type UnavailableExecutor struct{}

func (UnavailableExecutor) Provision(context.Context, Realm, func(ProvisionProgress)) (ProvisionResult, error) {
	return ProvisionResult{}, errors.New("realm operator is not configured")
}
func (UnavailableExecutor) InspectActivity(context.Context, Realm) (RealmActivity, error) {
	return RealmActivity{}, errors.New("realm operator is not configured")
}
func (UnavailableExecutor) Suspend(context.Context, Realm) error {
	return errors.New("realm operator is not configured")
}
func (UnavailableExecutor) Resume(context.Context, Realm) error {
	return errors.New("realm operator is not configured")
}
func (UnavailableExecutor) Delete(context.Context, Realm) error {
	return errors.New("realm operator is not configured")
}

// OperatorExecutor delegates to a separately deployed, narrowly privileged realm
// operator. It never runs a shell and sends only a fixed, signed command schema.
type OperatorExecutor struct {
	baseURL *url.URL
	secret  []byte
	http    *http.Client
}

func newOperatorExecutor(rawURL, secret string) (*OperatorExecutor, error) {
	u, err := url.Parse(rawURL)
	internalHTTP := u.Scheme == "http" && strings.HasSuffix(u.Hostname(), ".svc.cluster.local")
	if err != nil || (u.Scheme != "https" && !internalHTTP) || u.Host == "" || u.Path != "" {
		return nil, errors.New("operator URL must be HTTPS or an in-cluster service origin")
	}
	if len(secret) < 32 {
		return nil, errors.New("operator signing secret must contain at least 32 characters")
	}
	return &OperatorExecutor{baseURL: u, secret: []byte(secret), http: &http.Client{Timeout: 10 * time.Minute}}, nil
}

type operatorCommand struct {
	Action        string           `json:"action"`
	RealmID       string           `json:"realm_id"`
	RealmListID   uint32           `json:"realmlist_id"`
	Namespace     string           `json:"namespace"`
	PRNumber      int              `json:"pr_number"`
	CommitSHA     string           `json:"commit_sha"`
	Image         string           `json:"image"`
	ImporterImage string           `json:"importer_image"`
	Fixture       ProvisioningSpec `json:"fixture"`
	PurgeImage    bool             `json:"purge_image"`
	AdminUser     string           `json:"admin_user,omitempty"`
	AdminPass     string           `json:"admin_password,omitempty"`
}

func (e *OperatorExecutor) call(ctx context.Context, action string, realm Realm) error {
	_, err := e.callCommand(ctx, action, realm)
	return err
}

func (e *OperatorExecutor) callCommand(ctx context.Context, action string, realm Realm) ([]byte, error) {
	command := operatorCommand{Action: action, RealmID: realm.ID, RealmListID: realm.RealmListID, Namespace: realm.Namespace, PRNumber: realm.PRNumber, CommitSHA: realm.CommitSHA, Image: realm.Image, ImporterImage: realm.ImporterImage, Fixture: realm.Spec, PurgeImage: action == "delete"}
	b, _ := json.Marshal(command)
	ts := fmt.Sprint(time.Now().Unix())
	mac := hmac.New(sha256.New, e.secret)
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(b)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(e.baseURL.String(), "/")+"/v1/realm-commands", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TC9-Timestamp", ts)
	req.Header.Set("X-TC9-Signature", hex.EncodeToString(mac.Sum(nil)))
	resp, err := e.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("realm operator returned %s", resp.Status)
	}
	return body, nil
}

func (e *OperatorExecutor) Provision(ctx context.Context, r Realm, progress func(ProvisionProgress)) (ProvisionResult, error) {
	progress(ProvisionProgress{Phase: "operator", Percent: 2, Detail: "Waiting for the realm operator"})
	command := operatorCommand{Action: "provision", RealmID: r.ID, RealmListID: r.RealmListID, Namespace: r.Namespace, PRNumber: r.PRNumber, CommitSHA: r.CommitSHA, Image: r.Image, ImporterImage: r.ImporterImage, Fixture: r.Spec, AdminUser: "admin", AdminPass: "admin"}
	b, _ := json.Marshal(command)
	ts := fmt.Sprint(time.Now().Unix())
	mac := hmac.New(sha256.New, e.secret)
	mac.Write([]byte(ts + "."))
	mac.Write(b)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(e.baseURL.String(), "/")+"/v1/realm-commands", bytes.NewReader(b))
	if err != nil {
		return ProvisionResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TC9-Timestamp", ts)
	req.Header.Set("X-TC9-Signature", hex.EncodeToString(mac.Sum(nil)))
	resp, err := e.http.Do(req)
	if err != nil {
		return ProvisionResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		if resp.StatusCode == http.StatusUnprocessableEntity {
			return ProvisionResult{}, ErrCharacterFixturesUnavailable
		}
		return ProvisionResult{}, fmt.Errorf("realm operator returned %s", resp.Status)
	}
	var result ProvisionResult
	dec := json.NewDecoder(io.LimitReader(resp.Body, 256<<10))
	for {
		var event struct {
			Type string `json:"type"`
			ProvisionProgress
			Result *ProvisionResult `json:"result,omitempty"`
			Error  string           `json:"error,omitempty"`
			Code   string           `json:"code,omitempty"`
		}
		if err := dec.Decode(&event); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return ProvisionResult{}, fmt.Errorf("invalid realm operator progress stream: %w", err)
		}
		switch event.Type {
		case "progress":
			progress(event.ProvisionProgress)
		case "result":
			if event.Result != nil {
				result = *event.Result
			}
		case "error":
			if event.Code == "character_fixtures_unavailable" {
				return ProvisionResult{}, ErrCharacterFixturesUnavailable
			}
			return ProvisionResult{}, errors.New("realm operator: " + event.Error)
		}
	}
	if result.RealmName == "" || result.Address == "" || result.Port == 0 || result.RealmListID == 0 {
		return ProvisionResult{}, errors.New("realm operator did not return verified connection details")
	}
	return result, nil
}
func (e *OperatorExecutor) Suspend(ctx context.Context, r Realm) error {
	return e.call(ctx, "suspend", r)
}
func (e *OperatorExecutor) InspectActivity(ctx context.Context, r Realm) (RealmActivity, error) {
	body, err := e.callCommand(ctx, "inspect", r)
	if err != nil {
		return RealmActivity{}, err
	}
	var activity RealmActivity
	if err := json.Unmarshal(body, &activity); err != nil {
		return RealmActivity{}, fmt.Errorf("invalid realm activity response: %w", err)
	}
	if activity.ActiveConnections < 0 {
		return RealmActivity{}, errors.New("invalid negative active connection count")
	}
	return activity, nil
}
func (e *OperatorExecutor) Resume(ctx context.Context, r Realm) error {
	return e.call(ctx, "resume", r)
}
func (e *OperatorExecutor) Delete(ctx context.Context, r Realm) error {
	return e.call(ctx, "delete", r)
}
