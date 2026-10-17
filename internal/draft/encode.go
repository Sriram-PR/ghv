package draft

import (
	"bytes"
	"encoding/json"
	"time"
)

type wireDraft struct {
	Schema    int    `json:"schema"`
	Kind      Kind   `json:"kind"`
	Repo      string `json:"repo"`
	CreatedAt string `json:"created_at"`
	Fields    Fields `json:"fields"`
}

func Encode(d Draft) ([]byte, error) {
	if err := Validate(d); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	err := enc.Encode(wireDraft{
		Schema:    Schema,
		Kind:      d.Fields.Kind(),
		Repo:      d.Repo,
		CreatedAt: d.CreatedAt.UTC().Format(time.RFC3339Nano),
		Fields:    d.Fields,
	})
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
