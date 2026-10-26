package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

var errInvalidJSON = errors.New("invalid JSON object")

func decodeJSON(data []byte, target any) error {
	if !utf8.Valid(data) {
		return errInvalidJSON
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return errInvalidJSON
	}

	seen := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}

		key, ok := token.(string)
		if !ok || !validJSONField(key) || seen[key] {
			return errInvalidJSON
		}
		seen[key] = true

		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		if bytes.Equal(value, []byte("null")) && key != "assignee_id" {
			return errInvalidJSON
		}
	}

	if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
		return errInvalidJSON
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errInvalidJSON
	}

	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func validJSONField(name string) bool {
	if name == "" {
		return false
	}

	for _, char := range name {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '_') {
			return false
		}
	}

	return true
}
