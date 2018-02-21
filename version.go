package tcpproxy

// Version that is defined in git tag.
// There should be a release in GitHub with this tag.
var Version string

func init() {
	if Version == "" {
		Version = "v0.0.0"
	}
}
