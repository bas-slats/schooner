package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client wraps GitHub API operations
type Client struct {
	token      string
	httpClient *http.Client
}

// Repository represents a GitHub repository
type Repository struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	FullName      string    `json:"full_name"`
	Description   string    `json:"description"`
	Private       bool      `json:"private"`
	HTMLURL       string    `json:"html_url"`
	CloneURL      string    `json:"clone_url"`
	SSHURL        string    `json:"ssh_url"`
	DefaultBranch string    `json:"default_branch"`
	Language      string    `json:"language"`
	UpdatedAt     time.Time `json:"updated_at"`
	PushedAt      time.Time `json:"pushed_at"`
}

// NewClient creates a new GitHub client
func NewClient(token string) *Client {
	return &Client{
		token: token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetToken updates the GitHub token
func (c *Client) SetToken(token string) {
	c.token = token
}

// HasToken returns true if a token is configured
func (c *Client) HasToken() bool {
	return c.token != ""
}

// GetToken returns the current token
func (c *Client) GetToken() string {
	return c.token
}

// ListUserRepos lists repositories for the authenticated user
func (c *Client) ListUserRepos(ctx context.Context, page, perPage int) ([]Repository, error) {
	if c.token == "" {
		return nil, fmt.Errorf("GitHub token not configured")
	}

	if perPage <= 0 {
		perPage = 30
	}
	if page <= 0 {
		page = 1
	}

	url := fmt.Sprintf("https://api.github.com/user/repos?sort=pushed&direction=desc&per_page=%d&page=%d&affiliation=owner,collaborator,organization_member", perPage, page)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch repos: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error (status %d): %s", resp.StatusCode, string(body))
	}

	var repos []Repository
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return repos, nil
}

// GetRepo fetches details for a specific repository
func (c *Client) GetRepo(ctx context.Context, owner, repo string) (*Repository, error) {
	if c.token == "" {
		return nil, fmt.Errorf("GitHub token not configured")
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch repo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("repository not found")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error (status %d): %s", resp.StatusCode, string(body))
	}

	var repository Repository
	if err := json.NewDecoder(resp.Body).Decode(&repository); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &repository, nil
}

// GetUser fetches the authenticated user's info
func (c *Client) GetUser(ctx context.Context) (string, error) {
	if c.token == "" {
		return "", fmt.Errorf("GitHub token not configured")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API error (status %d): %s", resp.StatusCode, string(body))
	}

	var user struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return user.Login, nil
}

// CheckRepoHasDockerfile checks if a repo has a Dockerfile
func (c *Client) CheckRepoHasDockerfile(ctx context.Context, owner, repo string) (bool, error) {
	if c.token == "" {
		return false, fmt.Errorf("GitHub token not configured")
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/Dockerfile", owner, repo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to check Dockerfile: %w", err)
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

// CheckRepoHasDockerCompose checks if a repo has a docker-compose file
func (c *Client) CheckRepoHasDockerCompose(ctx context.Context, owner, repo string) (bool, string, error) {
	if c.token == "" {
		return false, "", fmt.Errorf("GitHub token not configured")
	}

	// Check common compose file names
	composeFiles := []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"}

	for _, filename := range composeFiles {
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", owner, repo, filename)

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			continue
		}

		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			return true, filename, nil
		}
	}

	return false, "", nil
}

// Webhook represents a GitHub webhook
type Webhook struct {
	ID     int64    `json:"id"`
	Name   string   `json:"name"`
	Active bool     `json:"active"`
	Events []string `json:"events"`
	Config struct {
		URL         string `json:"url"`
		ContentType string `json:"content_type"`
		InsecureSSL string `json:"insecure_ssl"`
	} `json:"config"`
}

// WebhookConfig contains configuration for creating a webhook
type WebhookConfig struct {
	URL         string
	Secret      string
	ContentType string
	Events      []string
}

// ListWebhooks lists webhooks for a repository
func (c *Client) ListWebhooks(ctx context.Context, owner, repo string) ([]Webhook, error) {
	if c.token == "" {
		return nil, fmt.Errorf("GitHub token not configured")
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/hooks", owner, repo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list webhooks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error (status %d): %s", resp.StatusCode, string(body))
	}

	var webhooks []Webhook
	if err := json.NewDecoder(resp.Body).Decode(&webhooks); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return webhooks, nil
}

// CreateWebhook creates a webhook for a repository
func (c *Client) CreateWebhook(ctx context.Context, owner, repo string, config WebhookConfig) (*Webhook, error) {
	if c.token == "" {
		return nil, fmt.Errorf("GitHub token not configured")
	}

	if config.ContentType == "" {
		config.ContentType = "json"
	}
	if len(config.Events) == 0 {
		config.Events = []string{"push"}
	}

	payload := map[string]interface{}{
		"name":   "web",
		"active": true,
		"events": config.Events,
		"config": map[string]string{
			"url":          config.URL,
			"content_type": config.ContentType,
			"insecure_ssl": "0",
		},
	}

	if config.Secret != "" {
		payload["config"].(map[string]string)["secret"] = config.Secret
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/hooks", owner, repo)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to create webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var webhook Webhook
	if err := json.NewDecoder(resp.Body).Decode(&webhook); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &webhook, nil
}

// DeleteWebhook deletes a webhook from a repository
func (c *Client) DeleteWebhook(ctx context.Context, owner, repo string, hookID int64) error {
	if c.token == "" {
		return fmt.Errorf("GitHub token not configured")
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/hooks/%d", owner, repo, hookID)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GitHub API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// EnsureWebhook ensures a webhook exists for the repository, creating it if needed
func (c *Client) EnsureWebhook(ctx context.Context, owner, repo, webhookURL, secret string) (*Webhook, bool, error) {
	if c.token == "" {
		return nil, false, fmt.Errorf("GitHub token not configured")
	}

	// List existing webhooks
	webhooks, err := c.ListWebhooks(ctx, owner, repo)
	if err != nil {
		return nil, false, fmt.Errorf("failed to list webhooks: %w", err)
	}

	// Check if webhook already exists
	for _, wh := range webhooks {
		if wh.Config.URL == webhookURL {
			return &wh, false, nil // Already exists
		}
	}

	// Create new webhook
	webhook, err := c.CreateWebhook(ctx, owner, repo, WebhookConfig{
		URL:    webhookURL,
		Secret: secret,
		Events: []string{"push"},
	})
	if err != nil {
		return nil, false, fmt.Errorf("failed to create webhook: %w", err)
	}

	return webhook, true, nil // Created new
}

// ParseRepoURL extracts owner and repo from a GitHub URL
func ParseRepoURL(repoURL string) (owner, repo string, err error) {
	// Handle various formats:
	// https://github.com/owner/repo.git
	// https://github.com/owner/repo
	// git@github.com:owner/repo.git
	// github.com/owner/repo

	repoURL = strings.TrimSuffix(repoURL, ".git")

	if strings.HasPrefix(repoURL, "git@github.com:") {
		parts := strings.Split(strings.TrimPrefix(repoURL, "git@github.com:"), "/")
		if len(parts) >= 2 {
			return parts[0], parts[1], nil
		}
	}

	if strings.Contains(repoURL, "github.com") {
		// Find github.com and extract what comes after
		idx := strings.Index(repoURL, "github.com")
		path := repoURL[idx+len("github.com"):]
		path = strings.TrimPrefix(path, "/")
		path = strings.TrimPrefix(path, ":")

		parts := strings.Split(path, "/")
		if len(parts) >= 2 {
			return parts[0], parts[1], nil
		}
	}

	return "", "", fmt.Errorf("could not parse GitHub repo URL: %s", repoURL)
}
