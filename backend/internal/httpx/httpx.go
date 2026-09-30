// Package httpx holds the JSON request/response helpers shared by the HTTP packages, so every
// endpoint uses the same error format: {"error":{"code":"...","message":"..."}}.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrorBody is the JSON body of every error response.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail carries a stable machine-readable code and a human-readable message.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteJSON writes v as JSON with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes an error response.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, ErrorBody{Error: ErrorDetail{Code: code, Message: message}})
}

// DefaultMaxBody is the request body limit used by DecodeJSON when maxBytes is zero.
const DefaultMaxBody = 1 << 20

// DecodeJSON decodes a single JSON object from the request body into dst. Unknown fields,
// trailing data and bodies over maxBytes are rejected. The returned error message is safe to
// show to the client.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBody
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		var typeErr *json.UnmarshalTypeError
		var syntaxErr *json.SyntaxError
		switch {
		case errors.As(err, &maxErr):
			return fmt.Errorf("request body is larger than %d bytes", maxBytes)
		case errors.Is(err, io.EOF):
			return errors.New("request body is empty")
		case errors.As(err, &typeErr):
			if typeErr.Field != "" {
				return fmt.Errorf("field %q must be %s", typeErr.Field, typeErr.Type)
			}
			return fmt.Errorf("body must be %s", typeErr.Type)
		case errors.As(err, &syntaxErr), errors.Is(err, io.ErrUnexpectedEOF):
			return errors.New("request body is not valid JSON")
		default:
			// Unknown fields: "json: unknown field \"x\"".
			return err
		}
	}
	if dec.More() {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}
