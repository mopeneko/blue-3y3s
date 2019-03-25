package cmdchecker

import (
	"strings"
)

func HasPrefixCommand(message string, prefixes []string) (string, bool) {
	for _, prefix := range prefixes {
		if strings.HasPrefix(message, prefix) {
			return prefix, true
		}
	}
	return "", false
}

func IsNormalCommand(cmd []string) bool {
	return len(cmd) == 2
}

func IsSettingCommand(cmd []string) bool {
	return len(cmd) == 3
}
