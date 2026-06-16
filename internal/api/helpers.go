package api

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/url"
)

// formBody returns an io.ReadCloser for an application/x-www-form-urlencoded
// body. The caller is responsible for closing it (http.Client does this for
// req.Body when it is non-nil).
func formBody(v url.Values) io.ReadCloser {
	return io.NopCloser(bytes.NewBufferString(v.Encode()))
}

// randRead wraps crypto/rand.Read for clarity at call sites.
func randRead(b []byte) (int, error) { return rand.Read(b) }

// base64URL encodes b as URL-safe base64 without padding.
func base64URL(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
