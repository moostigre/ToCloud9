package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type GitHubClient struct {
	http               *http.Client
	owner, repo, token string
}

func (g *GitHubClient) request(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+g.owner+"/"+g.repo+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}
	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("github returned %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(target)
}

type githubPR struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	HTMLURL   string    `json:"html_url"`
	State     string    `json:"state"`
	Draft     bool      `json:"draft"`
	UpdatedAt time.Time `json:"updated_at"`
	Head      struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

func pullRequest(raw githubPR) PullRequest {
	return PullRequest{Number: raw.Number, Title: raw.Title, Body: truncate(raw.Body, 12000), HTMLURL: raw.HTMLURL, HeadSHA: raw.Head.SHA, UpdatedAt: raw.UpdatedAt}
}

func (g *GitHubClient) OpenPR(ctx context.Context, number int) (PullRequest, error) {
	var raw githubPR
	if err := g.request(ctx, "/pulls/"+strconv.Itoa(number), &raw); err != nil {
		return PullRequest{}, err
	}
	if raw.State != "open" || raw.Draft || raw.Head.SHA == "" {
		return PullRequest{}, errors.New("only open, non-draft pull requests are eligible")
	}
	return pullRequest(raw), nil
}

func (g *GitHubClient) OpenPRs(ctx context.Context) ([]PullRequest, error) {
	var raw []githubPR
	if err := g.request(ctx, "/pulls?state=open&per_page=50&sort=updated&direction=desc", &raw); err != nil {
		return nil, err
	}
	out := make([]PullRequest, 0, len(raw))
	for _, pr := range raw {
		if !pr.Draft && pr.Head.SHA != "" {
			out = append(out, pullRequest(pr))
		}
	}
	return out, nil
}

type CatalogEntry struct {
	SHA           string `json:"sha"`
	Image         string `json:"image"`
	ImporterImage string `json:"importer_image"`
}

type ImageCatalog struct{ Path, AllowedPrefix, AllowedImporterPrefix string }

func (c ImageCatalog) Available() bool {
	b, err := os.ReadFile(c.Path)
	if err != nil {
		return false
	}
	var entries map[string]CatalogEntry
	return json.Unmarshal(b, &entries) == nil && len(entries) > 0
}

func (c ImageCatalog) Resolve(pr PullRequest) (CatalogEntry, error) {
	b, err := os.ReadFile(c.Path)
	if err != nil {
		return CatalogEntry{}, errors.New("trusted CI image catalogue is unavailable")
	}
	var entries map[string]CatalogEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		return CatalogEntry{}, errors.New("trusted CI image catalogue is invalid")
	}
	e, ok := entries[strconv.Itoa(pr.Number)]
	if !ok || e.SHA != pr.HeadSHA {
		return CatalogEntry{}, errors.New("the current PR commit has no approved image")
	}
	if !immutableImage(c.AllowedPrefix, e.Image) || !immutableImage(c.AllowedImporterPrefix, e.ImporterImage) {
		return CatalogEntry{}, errors.New("approved images must use allowed repositories and immutable digests")
	}
	return e, nil
}

func immutableImage(prefix, image string) bool {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(strings.TrimSuffix(prefix, "@")) + `@sha256:[a-f0-9]{64}$`).MatchString(image)
}

func (c ImageCatalog) Put(pr PullRequest, entry CatalogEntry) error {
	if entry.SHA != pr.HeadSHA || !immutableImage(c.AllowedPrefix, entry.Image) || !immutableImage(c.AllowedImporterPrefix, entry.ImporterImage) {
		return errors.New("catalogue entry is not eligible")
	}
	entries := map[string]CatalogEntry{}
	if b, err := os.ReadFile(c.Path); err == nil {
		if err := json.Unmarshal(b, &entries); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	entries[strconv.Itoa(pr.Number)] = entry
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.Path), 0700); err != nil {
		return err
	}
	tmp := c.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, c.Path)
}

func truncate(v string, max int) string {
	if len(v) > max {
		return v[:max]
	}
	return v
}
