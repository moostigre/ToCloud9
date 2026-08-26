package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Build struct {
	PRNumber    int       `json:"pr_number"`
	Status      string    `json:"status"`
	Step        string    `json:"step,omitempty"`
	StepStarted time.Time `json:"step_started_at,omitempty"`
	Progress    int       `json:"progress_percent"`
	Detail      string    `json:"progress_detail,omitempty"`
	UpdatedAt   time.Time `json:"progress_updated_at,omitempty"`
	URL         string    `json:"url"`
	Created     time.Time `json:"created_at"`
}

type BuildClient struct {
	http                  *http.Client
	owner, repo, workflow string
	ref, token            string
	maxActive             int
	mu                    sync.Mutex
	pending               map[int]time.Time
	cacheMu               sync.Mutex
	cached                []Build
	cacheAt               time.Time
}

type actionRun struct {
	ID           int64     `json:"id"`
	DisplayTitle string    `json:"display_title"`
	Status       string    `json:"status"`
	HTMLURL      string    `json:"html_url"`
	CreatedAt    time.Time `json:"created_at"`
}

type actionStep struct {
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	StartedAt *time.Time `json:"started_at"`
}

type actionJob struct {
	ID    int64        `json:"id"`
	Steps []actionStep `json:"steps"`
}

var buildTitlePattern = regexp.MustCompile(`(?i)PR\s*#?(\d+)`)
var compilerProgressPattern = regexp.MustCompile(`\[\s*(\d{1,3})%\]`)

func (b *BuildClient) enabled() bool { return b != nil && b.token != "" }

func (b *BuildClient) request(ctx context.Context, method, path string, body any, target any) error {
	if !b.enabled() {
		return errors.New("build service is unavailable")
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.github.com/repos/"+b.owner+"/"+b.repo+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+b.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := b.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return errors.New("PR build workflow is not activated on the fork's default branch")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("github actions returned %s", resp.Status)
	}
	if target != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(target)
	}
	return nil
}

func (b *BuildClient) Active(ctx context.Context) ([]Build, error) {
	if !b.enabled() {
		return nil, nil
	}
	b.cacheMu.Lock()
	if time.Since(b.cacheAt) < 10*time.Second {
		out := append([]Build(nil), b.cached...)
		b.cacheMu.Unlock()
		return out, nil
	}
	b.cacheMu.Unlock()
	var response struct {
		WorkflowRuns []actionRun `json:"workflow_runs"`
	}
	path := "/actions/workflows/" + b.workflow + "/runs?per_page=30"
	if err := b.request(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	out := make([]Build, 0, 2)
	for _, run := range response.WorkflowRuns {
		if run.Status != "queued" && run.Status != "in_progress" && run.Status != "pending" {
			continue
		}
		match := buildTitlePattern.FindStringSubmatch(run.DisplayTitle)
		if len(match) != 2 {
			continue
		}
		pr, _ := strconv.Atoi(match[1])
		build := Build{PRNumber: pr, Status: run.Status, URL: run.HTMLURL, Created: run.CreatedAt, Progress: 1, Detail: "Waiting for a GitHub Actions runner", UpdatedAt: run.CreatedAt}
		var jobs struct {
			Jobs []actionJob `json:"jobs"`
		}
		if err := b.request(ctx, http.MethodGet, "/actions/runs/"+strconv.FormatInt(run.ID, 10)+"/jobs?per_page=10", nil, &jobs); err == nil {
			for _, job := range jobs.Jobs {
				for _, step := range job.Steps {
					if step.Status == "in_progress" {
						build.Step = step.Name
						build.Progress, build.Detail = buildStepProgress(step.Name)
						if step.StartedAt != nil {
							build.StepStarted = *step.StartedAt
							build.UpdatedAt = *step.StartedAt
						}
						if strings.Contains(step.Name, "Build and publish runtime") {
							if pct, updated, ok := b.compilerProgress(ctx, job.ID); ok {
								build.Progress = 20 + pct*55/100
								build.Detail = fmt.Sprintf("Compiling AzerothCore runtime — compiler reported %d%%", pct)
								build.UpdatedAt = updated
							}
						}
					}
				}
			}
		}
		out = append(out, build)
	}
	b.cacheMu.Lock()
	b.cached, b.cacheAt = append([]Build(nil), out...), time.Now()
	b.cacheMu.Unlock()
	return out, nil
}

func (b *BuildClient) compilerProgress(ctx context.Context, jobID int64) (int, time.Time, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+b.owner+"/"+b.repo+"/actions/jobs/"+strconv.FormatInt(jobID, 10)+"/logs", nil)
	if err != nil {
		return 0, time.Time{}, false
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+b.token)
	resp, err := b.http.Do(req)
	if err != nil {
		return 0, time.Time{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return 0, time.Time{}, false
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return 0, time.Time{}, false
	}
	matches := compilerProgressPattern.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return 0, time.Time{}, false
	}
	pct, err := strconv.Atoi(string(matches[len(matches)-1][1]))
	updated, parseErr := http.ParseTime(resp.Header.Get("Last-Modified"))
	if parseErr != nil {
		updated = time.Now().UTC()
	}
	return pct, updated, err == nil && pct >= 0 && pct <= 100
}

func (b *BuildClient) Ensure(ctx context.Context, pr PullRequest) (Build, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending == nil {
		b.pending = map[int]time.Time{}
	}
	for number, queued := range b.pending {
		if time.Since(queued) >= 2*time.Minute {
			delete(b.pending, number)
		}
	}
	if queued, ok := b.pending[pr.Number]; ok {
		return Build{PRNumber: pr.Number, Status: "queued", URL: "https://github.com/" + b.owner + "/" + b.repo + "/actions", Created: queued, Progress: 1, Detail: "Waiting for GitHub Actions to register the build", UpdatedAt: queued}, nil
	}
	if len(b.pending) >= b.maxActive {
		return Build{}, fmt.Errorf("all %d build slots are currently occupied", b.maxActive)
	}
	active, err := b.Active(ctx)
	if err != nil {
		return Build{}, err
	}
	for _, build := range active {
		if build.PRNumber == pr.Number {
			delete(b.pending, pr.Number)
			return build, nil
		}
	}
	occupied := map[int]bool{}
	for _, build := range active {
		occupied[build.PRNumber] = true
	}
	for number := range b.pending {
		occupied[number] = true
	}
	if len(occupied) >= b.maxActive {
		return Build{}, fmt.Errorf("all %d build slots are currently occupied", b.maxActive)
	}
	body := map[string]any{"ref": b.ref, "inputs": map[string]string{"pr_number": strconv.Itoa(pr.Number)}}
	path := "/actions/workflows/" + b.workflow + "/dispatches"
	if err := b.request(ctx, http.MethodPost, path, body, nil); err != nil {
		return Build{}, err
	}
	now := time.Now().UTC()
	b.pending[pr.Number] = now
	return Build{PRNumber: pr.Number, Status: "queued", URL: "https://github.com/" + b.owner + "/" + b.repo + "/actions", Created: now, Progress: 1, Detail: "Build request accepted by GitHub Actions", UpdatedAt: now}, nil
}

func buildStepProgress(name string) (int, string) {
	switch {
	case strings.Contains(name, "checkout"):
		return 5, "Checking out the trusted build workflow"
	case strings.Contains(name, "Resolve eligible PR"):
		return 10, "Validating the PR and resolving its exact commit"
	case strings.Contains(name, "setup-buildx"):
		return 14, "Preparing the container builder"
	case strings.Contains(name, "login-action"):
		return 17, "Authenticating the private image publisher"
	case strings.Contains(name, "Build and publish runtime"):
		return 20, "Compiling AzerothCore and publishing the runtime image"
	case strings.Contains(name, "database importer"):
		return 78, "Building and publishing the PR database importer"
	case strings.Contains(name, "catalogue"):
		return 94, "Signing and publishing the immutable image catalogue entry"
	case strings.HasPrefix(name, "Post "):
		return 97, "Finalizing build caches and credentials"
	default:
		return 3, "Starting the secure PR image build"
	}
}

func sanitizeBuildConfig(v string) string { return strings.TrimSpace(v) }
