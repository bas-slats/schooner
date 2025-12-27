package build

import (
	"context"
	"io"

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
	ImageID   string
	ImageTag  string
	Size      int64
}
