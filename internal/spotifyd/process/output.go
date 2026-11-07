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

	authURLPrefix         = "Browse to: "
	retryableLoginFailure = "login failed with reason: try another access point"
)

var (
	failureMarkers = []string{
		"failed to connect to spotify",
		"discovery unavailable and no credentials found",
		"login failed with reason",
		"library not loaded",
		"librespot does not support",
		"could not initialize spirc",
	}
	versionPattern = regexp.MustCompile(`spotifyd (\d+)\.(\d+)\.(\d+)`)
	messagePrefix  = regexp.MustCompile(`^\s*(\[[A-Za-z]+\]\s*)?(\d+:\s+)?`)
)

func Classify(line string) Event {
	if strings.Contains(line, "Authenticated as") {
		return Authenticated
	}

	lower := strings.ToLower(strings.TrimSpace(line))
	if strings.HasPrefix(lower, "error:") && lower != "error:" {
		return Failed
	}
	if strings.Contains(lower, retryableLoginFailure) {
		return Other
	}
	for _, marker := range failureMarkers {
		if strings.Contains(lower, marker) {
			return Failed
		}
	}

	return Other
}

func Message(line string) string {
	return strings.TrimSpace(messagePrefix.ReplaceAllString(line, ""))
}

func AuthURL(line string) (string, bool) {
	_, url, found := strings.Cut(line, authURLPrefix)
	if !found {
		return "", false
	}

	url = strings.TrimSpace(url)
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
