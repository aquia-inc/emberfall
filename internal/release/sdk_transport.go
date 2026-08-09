package release

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
)

// ErrSDKResponseTooLarge identifies an SDK response that exceeded its allowed
// size. Its error value intentionally does not include response content.
var ErrSDKResponseTooLarge = errors.New("sdk response too large")

// ErrSDKResponseMalformed identifies a successful SDK response that was not a
// single, complete JSON value. Its error value intentionally does not include
// response content.
var ErrSDKResponseMalformed = errors.New("sdk response malformed")

// BoundedSDKTransport constrains and validates responses before they reach an
// SDK decoder. It is intentionally a narrow transport wrapper so callers can
// retain provider-specific SDK clients and authentication.
type BoundedSDKTransport struct {
	base     http.RoundTripper
	maxBytes int64
}

type sdkTransportError struct {
	operation string
	cause     error
}

func (e *sdkTransportError) Error() string {
	switch e.operation {
	case "read":
		return "read sdk response"
	case "close":
		return "close sdk response"
	default:
		return "sdk response operation failed"
	}
}

func (e *sdkTransportError) Unwrap() error {
	return e.cause
}

// NewBoundedSDKTransport returns a transport that accepts at most maxBytes in
// each response. A nil base uses http.DefaultTransport.
func NewBoundedSDKTransport(base http.RoundTripper, maxBytes int64) *BoundedSDKTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	if maxBytes < 0 {
		maxBytes = 0
	}
	return &BoundedSDKTransport{base: base, maxBytes: maxBytes}
}

// RoundTrip limits every response body, closes the original body, and restores
// the bounded body for the SDK. Successful responses must contain exactly one
// complete JSON value; non-successful responses are left for the SDK to decode
// into its provider-specific error type.
func (t *BoundedSDKTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err != nil || response == nil || response.Body == nil {
		return response, err
	}

	payload, oversize, readErr := readSDKResponse(response.Body, t.maxBytes)
	closeErr := response.Body.Close()
	if readErr != nil && closeErr != nil {
		return nil, errors.Join(
			&sdkTransportError{operation: "read", cause: readErr},
			&sdkTransportError{operation: "close", cause: closeErr},
		)
	}
	if readErr != nil {
		return nil, &sdkTransportError{operation: "read", cause: readErr}
	}
	if closeErr != nil {
		return nil, &sdkTransportError{operation: "close", cause: closeErr}
	}
	if oversize {
		return nil, fmt.Errorf("%w: limit %d bytes", ErrSDKResponseTooLarge, t.maxBytes)
	}
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		if err := validateCompleteJSON(payload); err != nil {
			return nil, ErrSDKResponseMalformed
		}
	}

	response.Body = io.NopCloser(bytes.NewReader(payload))
	response.ContentLength = int64(len(payload))
	return response, nil
}

func readSDKResponse(reader io.Reader, maxBytes int64) ([]byte, bool, error) {
	readLimit := maxBytes
	if readLimit < math.MaxInt64 {
		readLimit++
	}
	payload, err := io.ReadAll(io.LimitReader(reader, readLimit))
	if err != nil {
		return nil, false, err
	}
	if int64(len(payload)) > maxBytes {
		return nil, true, nil
	}
	return payload, false, nil
}

func validateCompleteJSON(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data")
	}
	return nil
}
