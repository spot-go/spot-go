package soloist

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

type (
	event int

	Build struct {
		Version string
		Built   time.Time
	}
)

const (
	other event = iota
	ready
	waitingForLogin
	loggedIn
	loginFailed
	failed

	buildLifetime = 90 * 24 * time.Hour
)

var (
	timestampPrefix = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}(\.\d+)?: `)
	deviceIDPattern = regexp.MustCompile(`device_id=(\S+)`)
	buildPattern    = regexp.MustCompile(`soloist (\S+) build (\d+)`)
)

func classify(line string) event {
	msg := message(line)
	switch {
	case strings.HasPrefix(msg, "ready,"):
		return ready
	case strings.HasPrefix(msg, "waiting for login"):
		return waitingForLogin
	case strings.HasPrefix(msg, "logged in as "):
		return loggedIn
	case strings.HasPrefix(msg, "login failed"):
		return loginFailed
	case strings.HasPrefix(msg, "Error:"):
		return failed
	default:
		return other
	}
}

func message(line string) string {
	return strings.TrimSpace(timestampPrefix.ReplaceAllString(strings.TrimSpace(line), ""))
}

func deviceID(line string) (string, bool) {
	m := deviceIDPattern.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	return m[1], true
}

func parseBuild(s string) (Build, bool) {
	m := buildPattern.FindStringSubmatch(s)
	if m == nil {
		return Build{}, false
	}

	seconds, err := strconv.ParseInt(m[2], 10, 64)
	if err != nil {
		return Build{}, false
	}
	return Build{Version: m[1], Built: time.Unix(seconds, 0)}, true
}

func (b Build) Expires() time.Time {
	return b.Built.Add(buildLifetime)
}
