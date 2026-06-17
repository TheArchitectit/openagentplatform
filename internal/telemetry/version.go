// Package telemetry provides build-time information about the running
// binary. The fields are populated at compile time via -ldflags:
//
//	-X github.com/openagentplatform/openagentplatform/internal/telemetry.Version=<semver>
//	-X github.com/openagentplatform/openagentplatform/internal/telemetry.CommitSHA=<sha>
//	-X github.com/openagentplatform/openagentplatform/internal/telemetry.BuildDate=<rfc3339>
package telemetry

import (
	"encoding/json"
	"runtime"
	"sync"
)

// BuildInfo describes the identity of the running binary. It is
// intentionally a small value type so it can be safely embedded in log
// fields, health responses, and audit events without allocation.
type BuildInfo struct {
	Version   string `json:"version"`
	CommitSHA string `json:"commit_sha"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
}

// These variables are overridden at link time via -ldflags. Sensible
// defaults let the binary still build (and run usefully in dev) without
// any build flags.
var (
	Version   = "dev"
	CommitSHA = "unknown"
	BuildDate = "unknown"
)

var (
	buildInfo     BuildInfo
	buildInfoOnce sync.Once
)

// GetBuildInfo returns the resolved build identity. It computes the
// value lazily on first call and caches it for the lifetime of the
// process. The Go runtime version is taken from runtime.Version() so it
// always reflects the actual toolchain that compiled the binary.
func GetBuildInfo() BuildInfo {
	buildInfoOnce.Do(func() {
		buildInfo = BuildInfo{
			Version:   Version,
			CommitSHA: CommitSHA,
			BuildDate: BuildDate,
			GoVersion: runtime.Version(),
		}
	})
	return buildInfo
}

// MarshalJSON returns the build info as a JSON object. It is provided as
// a method so callers can embed BuildInfo in larger response structs
// without worrying about encoding details.
func (b BuildInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Version   string `json:"version"`
		CommitSHA string `json:"commit_sha"`
		BuildDate string `json:"build_date"`
		GoVersion string `json:"go_version"`
	}{
		Version:   b.Version,
		CommitSHA: b.CommitSHA,
		BuildDate: b.BuildDate,
		GoVersion: b.GoVersion,
	})
}
