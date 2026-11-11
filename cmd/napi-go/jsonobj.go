package main

// jsonobj.go — a JSON object that keeps the order of its keys, to edit package.json files without
// reordering them.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// jsonObject is a JSON object with its keys in source order. Values are kept as written.
type jsonObject struct {
	keys   []string
	values map[string]json.RawMessage
}

func newJSONObject() *jsonObject {
	return &jsonObject{values: map[string]json.RawMessage{}}
}

// UnmarshalJSON reads an object, keeping the order of its keys. A repeated key keeps its first
// position and its last value, as in JavaScript.
func (o *jsonObject) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return errors.New("not a JSON object")
	}
	*o = *newJSONObject()
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := tok.(string)
		if !ok {
			return fmt.Errorf("unexpected token %v", tok)
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return err
		}
		o.setRaw(key, v)
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	return nil
}

// MarshalJSON writes the object compactly, keys in order, without escaping <, > and & (npm does
// not either).
func (o *jsonObject) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := marshalNoEscape(k)
		if err != nil {
			return nil, err
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(o.values[k])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// marshalNoEscape encodes v without HTML escaping and without the trailing newline of Encoder.
func marshalNoEscape(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

func (o *jsonObject) has(key string) bool {
	_, ok := o.values[key]
	return ok
}

func (o *jsonObject) setRaw(key string, v json.RawMessage) {
	if !o.has(key) {
		o.keys = append(o.keys, key)
	}
	o.values[key] = v
}

// set stores v under key: in place if the key exists, else at the end.
func (o *jsonObject) set(key string, v any) error {
	raw, err := marshalNoEscape(v)
	if err != nil {
		return err
	}
	o.setRaw(key, raw)
	return nil
}

func (o *jsonObject) remove(key string) {
	if !o.has(key) {
		return
	}
	delete(o.values, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

// get decodes the value of key into v. It reports whether the key exists.
func (o *jsonObject) get(key string, v any) (bool, error) {
	raw, ok := o.values[key]
	if !ok {
		return false, nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return true, fmt.Errorf("%q: %w", key, err)
	}
	return true, nil
}

// object returns the object stored under key, or a new empty one if key is missing.
func (o *jsonObject) object(key string) (*jsonObject, error) {
	sub := newJSONObject()
	if _, err := o.get(key, sub); err != nil {
		return nil, err
	}
	return sub, nil
}

// packageJSONIndent is the indentation of the package.json files written by npm.
const packageJSONIndent = "  "

// format returns the object indented like npm writes package.json, with a final newline.
func (o *jsonObject) format() ([]byte, error) {
	compact, err := o.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := json.Indent(&b, compact, "", packageJSONIndent); err != nil {
		return nil, err
	}
	b.WriteByte('\n')
	return b.Bytes(), nil
}

// readJSONObject reads a JSON object from a file.
func readJSONObject(path string) (*jsonObject, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	o := newJSONObject()
	if err := json.Unmarshal(data, o); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return o, nil
}

// writeJSONObject writes o to path, unless the file already has the same content.
func writeJSONObject(path string, o *jsonObject) error {
	data, err := o.format()
	if err != nil {
		return err
	}
	if cur, err := os.ReadFile(path); err == nil && bytes.Equal(cur, data) {
		return nil
	}
	return os.WriteFile(path, data, 0o644)
}
