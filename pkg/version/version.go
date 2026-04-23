package version

import "fmt"

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func Full() string {
	return fmt.Sprintf("upuai/%s (%s) built %s", Version, Commit, BuildDate)
}

func Short() string {
	return Version
}
