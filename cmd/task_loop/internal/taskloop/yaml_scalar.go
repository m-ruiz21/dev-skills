package taskloop

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

func quoteYAMLScalar(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("value is not valid UTF-8")
	}
	quoted, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	var escaped strings.Builder
	for _, character := range string(quoted) {
		if character >= '\u007f' && character <= '\u009f' {
			fmt.Fprintf(&escaped, `\u%04x`, character)
		} else {
			escaped.WriteRune(character)
		}
	}
	return escaped.String(), nil
}

func unquoteYAMLScalar(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		var decoded string
		if json.Unmarshal([]byte(value), &decoded) == nil {
			return decoded
		}
	}
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	}
	return value
}
