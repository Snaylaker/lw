package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Record is a JSON object decoded one field at a time by the shape helpers.
// Rewrites use typed structs, so retaining the source document's key order here
// would be dead state.
type Record map[string]json.RawMessage

// Get returns nil for an absent key, which every shape helper reads as "wrong shape".
func (r Record) Get(key string) json.RawMessage { return r[key] }

type jsonKind int

const (
	kindInvalid jsonKind = iota
	kindObject
	kindArray
	kindString
	kindNumber
	kindBool
	kindNull
)

func kindOf(raw json.RawMessage) jsonKind {
	for _, b := range raw {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		case '{':
			return kindObject
		case '[':
			return kindArray
		case '"':
			return kindString
		case 't', 'f':
			return kindBool
		case 'n':
			return kindNull
		}
		if b == '-' || (b >= '0' && b <= '9') {
			return kindNumber
		}
		return kindInvalid
	}
	return kindInvalid
}

// AsRecord yields the value as a JSON object; ok is false for null, arrays and
// scalars. encoding/json already gives duplicated keys their last value.
func AsRecord(raw json.RawMessage) (Record, bool) {
	if kindOf(raw) != kindObject {
		return nil, false
	}
	var record Record
	if err := json.Unmarshal(raw, &record); err != nil || record == nil {
		return nil, false
	}
	return record, true
}

// AsString yields the value only when it is a JSON string.
func AsString(raw json.RawMessage) (string, bool) {
	if kindOf(raw) != kindString {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

// AsNumber yields the value only when it is a finite JSON number. A literal
// outside float64 range fails: a non-finite literal is not a usable number.
func AsNumber(raw json.RawMessage) (float64, bool) {
	if kindOf(raw) != kindNumber {
		return 0, false
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, false
	}
	return value, true
}

// AsBool yields the value only when it is a JSON boolean. A string "true" is
// not a boolean: an entry of the wrong type is dropped rather than guessed at,
// and for a flag that deletes checkouts the dropped value is the safe one.
func AsBool(raw json.RawMessage) (bool, bool) {
	if kindOf(raw) != kindBool {
		return false, false
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, false
	}
	return value, true
}

// AsArray yields the elements only when the value is a JSON array.
func AsArray(raw json.RawMessage) ([]json.RawMessage, bool) {
	if kindOf(raw) != kindArray {
		return nil, false
	}
	var value []json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, false
	}
	if value == nil {
		value = []json.RawMessage{}
	}
	return value, true
}

func describeJSONValue(raw json.RawMessage) string {
	switch kindOf(raw) {
	case kindNull:
		return "null"
	case kindArray:
		return "an array"
	case kindString:
		return "a string"
	case kindNumber:
		return "a number"
	case kindBool:
		return "a boolean"
	}
	return "an object"
}

// MarshalCompact is the compact form: no indentation, no trailing newline, and
// no HTML escaping of <, > or &.
func MarshalCompact(value any) ([]byte, error) {
	encoded, err := encode(value, "")
	if err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(encoded, []byte("\n")), nil
}

// MarshalIndented is two-space indent with a trailing newline.
func MarshalIndented(value any) ([]byte, error) {
	return encode(value, "  ")
}

func encode(value any, indent string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent != "" {
		enc.SetIndent("", indent)
	}
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// AtomicWriteJSON writes through a temp file in the destination directory and
// renames: a crash mid-write must never leave a truncated config or metadata
// entry. Directories are 0700 and the file 0600, both subject to the umask.
func AtomicWriteJSON(path string, payload []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp-%d", path, os.Getpid())
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(payload)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
