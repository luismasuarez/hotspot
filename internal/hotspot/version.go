package hotspot

// version is overridden at build time by GoReleaser with
// -ldflags "-X github.com/luismasuarez/hotspot/internal/hotspot.version=vX.Y.Z".
var version = "dev"

// Version returns the build version of the binary.
func Version() string {
	return version
}
