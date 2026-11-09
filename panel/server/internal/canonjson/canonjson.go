// Package canonjson encodes values in the canonical JSON form that the
// read-only exports hash: section and snapshot hashes of GET /api/v1/config
// and the knowledge base revision of GET /api/v1/kb.
package canonjson

import (
	"bytes"
	"encoding/json"
)

// Marshal encodes v with object keys sorted at every level, no
// insignificant whitespace and no HTML escaping: strings are UTF-8 as is,
// except that '"' and '\\' are escaped, control characters below U+0020 are
// \b \f \n \r \t or \u00xx (lowercase hex), and U+2028 / U+2029 are written as
// the escapes   /   (Go's encoder always does that). Integers are
// kept as written. The README gives the byte-identical Python recipe
// (TestCanonicalJSON_DocumentedForm in internal/api).
func Marshal(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(generic); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
