package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const NewsURL = "http://163.172.51.144:3000/api/pages?type=news"

type NewsItem struct {
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
}

func FetchNews() ([]NewsItem, error) {
	httpClient := &http.Client{Timeout: 8 * time.Second}
	response, err := httpClient.Get(NewsURL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("news server returned %s", response.Status)
	}
	var payload struct {
		Pages []NewsItem `json:"pages"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Pages, nil
}
