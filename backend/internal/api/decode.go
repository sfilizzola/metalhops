package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"
)

var ErrUnsupportedMediaType = errors.New("api: unsupported media type")
var ErrBodyTooLarge = errors.New("api: body too large")
var ErrMalformedBody = errors.New("api: malformed body")

type optionalString struct {
	Set   bool
	Null  bool
	Value string
}

func (o *optionalString) UnmarshalJSON(data []byte) error {
	o.Set = true
	if bytes.Equal(data, []byte("null")) {
		o.Null = true
		return nil
	}
	return json.Unmarshal(data, &o.Value)
}

func DecodeJSON(w http.ResponseWriter, r *http.Request, dest any) error {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return ErrUnsupportedMediaType
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<16))
	if err != nil {
		var max *http.MaxBytesError
		if errors.As(err, &max) {
			return ErrBodyTooLarge
		}
		return ErrMalformedBody
	}
	if !utf8.Valid(body) {
		return ErrMalformedBody
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return ErrMalformedBody
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return ErrMalformedBody
	}
	return nil
}
