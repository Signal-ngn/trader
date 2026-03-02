package engine

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
)

// hmacSHA256 signs the given message with the secret using HMAC-SHA256.
// Returns the hex-encoded signature.
func hmacSHA256(secret, message string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	b := mac.Sum(nil)
	return fmt.Sprintf("%x", b)
}

// decodeJSON decodes a JSON response body into v.
func decodeJSON(r io.Reader, v interface{}) error {
	return json.NewDecoder(r).Decode(v)
}
