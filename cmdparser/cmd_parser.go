package cmdparser

import (
	"strings"
)

func ParsePhrases(message string, prefix string) []string {
	splitedMessage := strings.SplitN(message, prefix, 2)
	phrases := strings.Split(splitedMessage[1], ":")
	phrases, phrases[0] = append(phrases[0:1], phrases[0:]...), splitedMessage[0]
	return phrases
}

func ParseCommand(phrases []string) string {
	return phrases[1]
}
