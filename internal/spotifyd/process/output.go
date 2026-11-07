package process

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type (
	Event int

	Version struct {
		Major, Minor, Patch int
	}
)

const (
	Other Event = iota
	Authenticated
	Failed

	authURLPrefix = "Browse to: "
)

var (
	failureMarkers = []string{
		"failed to connect to spotify",
		"discovery unavailable and no credentials found",
		"library not loaded",
		"librespot does not support",
		"could not initialize spirc",
	}
	versionPattern = regexp.MustCompile(`spotifyd (\d+)\.(\d+)\.(\d+)`)
)

func Classify(line string) Event {
	if strings.Contains(line, "Authenticated as") {
		return Authenticated
	}

	lower := strings.ToLower(strings.TrimSpace(line))
	if strings.HasPrefix(lower, "error:") && lower != "error:" {
		return Failed
	}
	for _, marker := range failureMarkers {
		if strings.Contains(lower, marker) {
			return Failed
		}
	}

	return Other
}

func AuthURL(line string) (string, bool) {
	i := strings.Index(line, authURLPrefix)
	if i < 0 {
		return "", false
	}

	url := strings.TrimSpace(line[i+len(authURLPrefix):])
	return url, url != ""
}

func StalePattern(cachePath string) string {
	return "spotifyd .*--cache-path " + regexp.QuoteMeta(cachePath) + "( |$)"
}

func ParseVersion(s string) (Version, bool) {
	m := versionPattern.FindStringSubmatch(s)
	if m == nil {
		return Version{}, false
	}

	var v Version
	for i, dst := range []*int{&v.Major, &v.Minor, &v.Patch} {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return Version{}, false
		}
		*dst = n
	}
	return v, true
}

func (v Version) AtLeast(o Version) bool {
	if v.Major != o.Major {
		return v.Major > o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor > o.Minor
	}
	return v.Patch >= o.Patch
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}
