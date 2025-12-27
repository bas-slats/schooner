package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// Client provides git operations
type Client struct {
	workDir string
	auth    transport.AuthMethod
	logger  *slog.Logger
}

// ClientOption configures the git client
type ClientOption func(*Client)

// WithAuth sets the authentication method
func WithAuth(auth transport.AuthMethod) ClientOption {
	return func(c *Client) {
		c.auth = auth
	}
}

// WithHTTPAuth sets HTTP basic authentication
func WithHTTPAuth(username, token string) ClientOption {
	return func(c *Client) {
		c.auth = &http.BasicAuth{
			Username: username,
			Password: token,
		}
	}
}

// WithSSHKey sets SSH key authentication
func WithSSHKey(keyPath string) ClientOption {
	return func(c *Client) {
		auth, err := ssh.NewPublicKeysFromFile("git", keyPath, "")
		if err != nil {
			c.logger.Error("failed to load SSH key", "path", keyPath, "error", err)
			return
		}
		c.auth = auth
	}
}

// NewClient creates a new git client
func NewClient(workDir string, opts ...ClientOption) (*Client, error) {
	// Ensure work directory exists
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create work directory: %w", err)
	}

	c := &Client{
		workDir: workDir,
		logger:  slog.Default(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

// CloneOptions configures clone/pull operations
type CloneOptions struct {
	URL      string
	Branch   string
	Depth    int
	Progress io.Writer
}

// CloneOrPull clones a repository if it doesn't exist, or pulls updates
func (c *Client) CloneOrPull(ctx context.Context, opts CloneOptions) (*git.Repository, error) {
	repoPath := c.RepoPath(opts.URL)

	// Check if repo already exists
	if _, err := os.Stat(filepath.Join(repoPath, ".git")); err == nil {
		return c.pull(ctx, repoPath, opts)
	}

	return c.clone(ctx, repoPath, opts)
}

// clone clones a new repository
func (c *Client) clone(ctx context.Context, path string, opts CloneOptions) (*git.Repository, error) {
	c.logger.Info("cloning repository", "url", opts.URL, "branch", opts.Branch)

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("failed to create parent directory: %w", err)
	}

	cloneOpts := &git.CloneOptions{
		URL:           opts.URL,
		Auth:          c.auth,
		ReferenceName: plumbing.NewBranchReferenceName(opts.Branch),
		SingleBranch:  true,
		Progress:      opts.Progress,
	}

	if opts.Depth > 0 {
		cloneOpts.Depth = opts.Depth
	}

	repo, err := git.PlainCloneContext(ctx, path, false, cloneOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}

	c.logger.Info("repository cloned", "path", path)
	return repo, nil
}

// pull pulls updates for an existing repository
func (c *Client) pull(ctx context.Context, path string, opts CloneOptions) (*git.Repository, error) {
	c.logger.Info("pulling repository", "path", path, "branch", opts.Branch)

	repo, err := git.PlainOpen(path)
	if err != nil {
		// If repo is corrupted, remove and re-clone
		c.logger.Warn("failed to open repository, will re-clone", "error", err)
		if err := os.RemoveAll(path); err != nil {
			return nil, fmt.Errorf("failed to remove corrupted repo: %w", err)
		}
		return c.clone(ctx, path, opts)
	}

	w, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	// Fetch first to get latest refs
	fetchOpts := &git.FetchOptions{
		RemoteName: "origin",
		Auth:       c.auth,
		Progress:   opts.Progress,
		Force:      true,
	}

	if err := repo.FetchContext(ctx, fetchOpts); err != nil && err != git.NoErrAlreadyUpToDate {
		c.logger.Warn("fetch failed", "error", err)
	}

	// Checkout and reset to the target branch
	branchRef := plumbing.NewBranchReferenceName(opts.Branch)
	remoteRef := plumbing.NewRemoteReferenceName("origin", opts.Branch)

	// Get remote ref
	ref, err := repo.Reference(remoteRef, true)
	if err != nil {
		return nil, fmt.Errorf("failed to get remote reference: %w", err)
	}

	// Reset to remote HEAD
	if err := w.Reset(&git.ResetOptions{
		Commit: ref.Hash(),
		Mode:   git.HardReset,
	}); err != nil {
		return nil, fmt.Errorf("failed to reset: %w", err)
	}

	// Update local branch reference
	localRef := plumbing.NewHashReference(branchRef, ref.Hash())
	if err := repo.Storer.SetReference(localRef); err != nil {
		c.logger.Warn("failed to update local branch ref", "error", err)
	}

	c.logger.Info("repository updated", "path", path)
	return repo, nil
}

// GetHeadCommit returns the HEAD commit
func (c *Client) GetHeadCommit(repo *git.Repository) (*object.Commit, error) {
	ref, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD: %w", err)
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to get commit: %w", err)
	}

	return commit, nil
}

// RepoPath returns the local path for a repository URL
func (c *Client) RepoPath(url string) string {
	// Create a safe directory name from the URL
	// e.g., "https://github.com/user/repo.git" -> "github.com_user_repo"
	name := url
	name = strings.TrimPrefix(name, "https://")
	name = strings.TrimPrefix(name, "http://")
	name = strings.TrimPrefix(name, "git@")
	name = strings.TrimSuffix(name, ".git")
	name = strings.ReplaceAll(name, ":", "_")
	name = strings.ReplaceAll(name, "/", "_")

	// Add a hash suffix for uniqueness
	hash := sha256.Sum256([]byte(url))
	hashStr := hex.EncodeToString(hash[:4])

	return filepath.Join(c.workDir, fmt.Sprintf("%s_%s", name, hashStr))
}

// Clean removes a repository from local storage
func (c *Client) Clean(url string) error {
	path := c.RepoPath(url)
	return os.RemoveAll(path)
}

// ListLocalRepos returns all locally cloned repositories
func (c *Client) ListLocalRepos() ([]string, error) {
	entries, err := os.ReadDir(c.workDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var repos []string
	for _, entry := range entries {
		if entry.IsDir() {
			gitPath := filepath.Join(c.workDir, entry.Name(), ".git")
			if _, err := os.Stat(gitPath); err == nil {
				repos = append(repos, entry.Name())
			}
		}
	}

	return repos, nil
}

// GetRemoteURL returns the origin remote URL for a repo path
func (c *Client) GetRemoteURL(repoPath string) (string, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", err
	}

	remote, err := repo.Remote("origin")
	if err != nil {
		return "", err
	}

	cfg := remote.Config()
	if len(cfg.URLs) > 0 {
		return cfg.URLs[0], nil
	}

	return "", fmt.Errorf("no remote URL found")
}

// SetRemoteURL updates the origin remote URL
func (c *Client) SetRemoteURL(repoPath, url string) error {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return err
	}

	// Delete existing origin
	_ = repo.DeleteRemote("origin")

	// Create new origin
	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{url},
	})

	return err
}
