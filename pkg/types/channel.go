package types

import (
	"fmt"
	"regexp"
	"strings"
)

const DefaultChannel = "main"

var channelPattern = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)

// NormalizeChannel validates the provided channel and returns DefaultChannel when empty.
func NormalizeChannel(channel string) (string, error) {
	cleaned := strings.TrimSpace(channel)
	if cleaned == "" {
		return DefaultChannel, nil
	}
	if channelPattern.MatchString(cleaned) {
		return cleaned, nil
	}
	return "", fmt.Errorf("invalid channel %q", channel)
}
