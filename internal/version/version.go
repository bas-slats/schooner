package version

// These are set at build time via ldflags
var (
	// Commit is the git commit hash
	Commit = "dev"
)

// GetShortCommit returns first 8 chars of commit hash
func GetShortCommit() string {
	if len(Commit) > 8 {
		return Commit[:8]
	}
	return Commit
}
