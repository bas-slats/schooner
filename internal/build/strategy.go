package build

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"schooner/internal/models"
)

// Strategy defines the interface for build methods
type Strategy interface {
	// Name returns the strategy name
	Name() models.BuildStrategy

	// Build executes the build and returns the image tag
	Build(ctx context.Context, opts BuildOptions) (*BuildResult, error)

	// Validate checks if the strategy can be used
	Validate(ctx context.Context, opts BuildOptions) error
}

// BuildOptions contains options for building
type BuildOptions struct {
	AppID        string
	AppName      string
	BuildID      string
	RepoPath     string
	ImageName    string
	Tag          string
	BuildContext string
	Dockerfile   string
	ComposeFile  string
	EnvVars      map[string]string
	BuildArgs    map[string]string
	LogWriter    io.Writer
}

// BuildResult contains the result of a build
type BuildResult struct {
	ImageID  string
	ImageTag string
	Size     int64
}

// SafePath validates that a user-supplied path doesn't escape the base directory.
// Returns the cleaned absolute path or an error if the path is invalid.
func SafePath(basePath, userPath string) (string, error) {
	// Clean and normalize the user path
	cleaned := filepath.Clean(userPath)

	// Reject absolute paths
	if filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("absolute paths not allowed: %s", userPath)
	}

	// Reject paths with parent directory traversal
	if strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, string(filepath.Separator)+"..") {
		return "", fmt.Errorf("path traversal not allowed: %s", userPath)
	}

	// Join with base and verify it's still under base
	fullPath := filepath.Join(basePath, cleaned)
	absBase, err := filepath.Abs(basePath)
	if err != nil {
		return "", fmt.Errorf("invalid base path: %w", err)
	}
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	// Ensure the resulting path is under the base directory
	if !strings.HasPrefix(absPath, absBase+string(filepath.Separator)) && absPath != absBase {
		return "", fmt.Errorf("path escapes base directory: %s", userPath)
	}

	return fullPath, nil
}
