// Package github handles GitHub repository synchronization.
package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Client manages GitHub repository operations.
type Client struct {
	repoURL  string
	pat      string
	branch   string
	cacheDir string
	apiBase  string
	logger   *slog.Logger
}

// Config holds GitHub client configuration.
type Config struct {
	RepoURL  string // e.g., "https://github.com/owner/repo"
	PAT      string // Personal Access Token (optional for public repos)
	Branch   string // e.g., "main"
	CacheDir string // Local directory to clone/pull into
	Logger   *slog.Logger
}

// branchNameRegex validates Git branch names to prevent command injection.
// Allows alphanumeric, hyphens, underscores, forward slashes, and dots
// but prevents special shell characters and command separators.
var branchNameRegex = regexp.MustCompile(`^[a-zA-Z0-9/_.-]+$`)

// validateBranchName validates a Git branch name to prevent command injection.
// Returns an error if the branch name contains potentially dangerous characters.
func validateBranchName(branch string) error {
	if branch == "" {
		return errors.New("branch name cannot be empty")
	}

	// Check for shell metacharacters and command separators
	if strings.ContainsAny(branch, ";|&$`()<>\"'\\") {
		return fmt.Errorf("branch name contains invalid characters: %s", branch)
	}

	// Check against whitelist regex
	if !branchNameRegex.MatchString(branch) {
		return fmt.Errorf("branch name contains invalid characters (only alphanumeric, -, _, /, . allowed): %s", branch)
	}

	// Prevent directory traversal
	if strings.Contains(branch, "..") {
		return errors.New("branch name cannot contain '..'")
	}

	// Prevent branches starting with special characters
	if strings.HasPrefix(branch, "-") {
		return errors.New("branch name cannot start with '-'")
	}

	return nil
}

// NewClient creates a new GitHub client.
// Returns an error if the branch name is invalid (contains shell metacharacters).
func NewClient(cfg Config) (*Client, error) {
	// SECURITY: Validate branch name to prevent command injection
	if err := validateBranchName(cfg.Branch); err != nil {
		return nil, fmt.Errorf("invalid branch name: %w", err)
	}

	apiBase := extractAPIURL(cfg.RepoURL)
	return &Client{
		repoURL:  cfg.RepoURL,
		pat:      cfg.PAT,
		branch:   cfg.Branch,
		cacheDir: cfg.CacheDir,
		apiBase:  apiBase,
		logger:   cfg.Logger,
	}, nil
}

// extractAPIURL converts a GitHub repo URL to the API URL.
// https://github.com/owner/repo -> https://api.github.com/repos/owner/repo
func extractAPIURL(repoURL string) string {
	parts := strings.TrimPrefix(repoURL, "https://github.com/")
	parts = strings.TrimSuffix(parts, ".git")
	parts = strings.Trim(parts, "/")
	return fmt.Sprintf("https://api.github.com/repos/%s", parts)
}

// CommitInfo contains information about a commit.
type CommitInfo struct {
	SHA     string    `json:"sha"`
	Date    time.Time `json:"date"`
	Message string    `json:"message"`
}

// GetLatestCommitSHA fetches the latest commit SHA from GitHub API.
func (c *Client) GetLatestCommitSHA() (string, error) {
	url := fmt.Sprintf("%s/commits/%s", c.apiBase, c.branch)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	if c.pat != "" {
		req.Header.Set("Authorization", "token "+c.pat)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 10 * time.Second}

	// Retry logic with exponential backoff
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt)) * 2 * time.Second
			time.Sleep(backoff)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			c.logger.Warn("GitHub API request failed",
				"attempt", attempt+1,
				"max_attempts", 3,
				"error", err,
			)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusForbidden {
			return "", fmt.Errorf("GitHub API rate limit or authentication failure (status %d)", resp.StatusCode)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return "", fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
		}

		var result struct {
			SHA string `json:"sha"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return "", fmt.Errorf("failed to decode response: %w", err)
		}

		return result.SHA, nil
	}

	return "", fmt.Errorf("failed after 3 attempts: %w", lastErr)
}

// Sync synchronizes the local repository cache with the remote.
// Returns true if the repository was updated, false if already up-to-date.
func (c *Client) Sync() (bool, error) {
	// Ensure cache directory exists
	if err := os.MkdirAll(c.cacheDir, 0750); err != nil {
		return false, fmt.Errorf("failed to create cache directory: %w", err)
	}

	gitDir := filepath.Join(c.cacheDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		// Clone repository
		c.logger.Info("Cloning repository", "path", c.cacheDir)
		return true, c.clone()
	}

	// Pull changes
	c.logger.Info("Pulling changes", "path", c.cacheDir)
	return c.pull()
}

// clone performs a shallow clone of the repository.
func (c *Client) clone() error {
	// Build clone URL with PAT if available
	cloneURL := c.repoURL
	if c.pat != "" {
		cloneURL = strings.Replace(c.repoURL, "https://", fmt.Sprintf("https://%s@", c.pat), 1)
	}

	cmd := exec.Command("git", "clone", "--depth", "1", "--branch", c.branch, cloneURL, c.cacheDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Sanitize output to avoid leaking PAT
		safeOutput := c.sanitizeOutput(string(output))
		return fmt.Errorf("git clone failed: %s", safeOutput)
	}

	c.logger.Info("Repository cloned successfully")
	return nil
}

// pull fetches and resets to the latest remote state.
// Returns true if the repository was updated.
func (c *Client) pull() (bool, error) {
	// Get current commit SHA
	oldSHA, err := c.getCurrentSHA()
	if err != nil {
		return false, fmt.Errorf("failed to get current SHA: %w", err)
	}

	// Fetch latest changes
	cmd := exec.Command("git", "-C", c.cacheDir, "fetch", "origin", c.branch)
	if output, err := cmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("git fetch failed: %s", c.sanitizeOutput(string(output)))
	}

	// Reset to remote state (discards any local changes)
	cmd = exec.Command("git", "-C", c.cacheDir, "reset", "--hard", fmt.Sprintf("origin/%s", c.branch))
	if output, err := cmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("git reset failed: %s", c.sanitizeOutput(string(output)))
	}

	// Get new commit SHA
	newSHA, err := c.getCurrentSHA()
	if err != nil {
		return false, fmt.Errorf("failed to get new SHA: %w", err)
	}

	updated := oldSHA != newSHA
	if updated {
		c.logger.Info("Repository updated",
			"old_sha", truncateSHA(oldSHA),
			"new_sha", truncateSHA(newSHA),
		)
	} else {
		c.logger.Info("Repository already up-to-date", "sha", truncateSHA(oldSHA))
	}

	return updated, nil
}

// truncateSHA safely returns the first 7 characters of a SHA, or the full SHA if shorter.
func truncateSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// getCurrentSHA returns the current HEAD commit SHA.
func (c *Client) getCurrentSHA() (string, error) {
	cmd := exec.Command("git", "-C", c.cacheDir, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// sanitizeOutput removes the PAT from output strings.
func (c *Client) sanitizeOutput(output string) string {
	if c.pat != "" {
		return strings.ReplaceAll(output, c.pat, "***")
	}
	return output
}

// GetCacheDir returns the local cache directory path.
func (c *Client) GetCacheDir() string {
	return c.cacheDir
}
