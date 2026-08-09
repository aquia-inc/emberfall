package release

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBoundedSDKTransportAcceptsExactLimitAndPreservesResponseMetadata(t *testing.T) {
	payload := `{"ok":true}`
	body := &transportTrackingReadCloser{Reader: strings.NewReader(payload)}
	request := httptest.NewRequest(http.MethodGet, "https://example.test/releases", nil)
	responseHeader := make(http.Header)
	responseHeader.Set("X-Request-ID", "request-123")
	transport := NewBoundedSDKTransport(transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			Status:        http.StatusText(http.StatusCreated),
			StatusCode:    http.StatusCreated,
			Header:        responseHeader,
			Body:          body,
			ContentLength: int64(len(payload)),
			Request:       request,
		}, nil
	}), int64(len(payload)))

	got, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	gotPayload, err := io.ReadAll(got.Body)
	if err != nil {
		t.Fatalf("read restored body: %v", err)
	}
	if string(gotPayload) != payload {
		t.Errorf("restored body = %q, want %q", gotPayload, payload)
	}
	if got.StatusCode != http.StatusCreated || got.Header.Get("X-Request-ID") != "request-123" || got.Request != request {
		t.Errorf("response metadata = %#v, want preserved status, headers, and request", got)
	}
	if !body.closed {
		t.Error("original response body was not closed")
	}
}

func TestBoundedSDKTransportRejectsOversizeResponseWithoutLeakingBodyOrCredentials(t *testing.T) {
	const limit = int64(8)
	const secretBody = "response-secret"
	const credential = "credential-secret"
	body := &transportTrackingReadCloser{Reader: strings.NewReader(secretBody)}
	transport := NewBoundedSDKTransport(transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
	}), limit)
	request := httptest.NewRequest(http.MethodGet, "https://example.test/releases", nil)
	request.Header.Set("Authorization", "Bearer "+credential)

	response, err := transport.RoundTrip(request)
	if response != nil {
		t.Errorf("response = %#v, want nil", response)
	}
	if !errors.Is(err, ErrSDKResponseTooLarge) {
		t.Fatalf("RoundTrip error = %v, want ErrSDKResponseTooLarge", err)
	}
	if strings.Contains(err.Error(), secretBody) || strings.Contains(err.Error(), credential) {
		t.Errorf("error leaked sensitive data: %v", err)
	}
	if !body.closed {
		t.Error("original response body was not closed")
	}
	if body.read > limit+1 {
		t.Errorf("read %d bytes, want at most %d", body.read, limit+1)
	}
}

func TestBoundedSDKTransportRejectsMalformedOrTrailingSuccessfulJSON(t *testing.T) {
	for _, payload := range []string{
		`{"token":"response-secret"`,
		`{"ok":true} response-secret`,
	} {
		t.Run(payload, func(t *testing.T) {
			body := &transportTrackingReadCloser{Reader: strings.NewReader(payload)}
			transport := NewBoundedSDKTransport(transportFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
			}), 1<<20)

			response, err := transport.RoundTrip(httptest.NewRequest(http.MethodGet, "https://example.test/releases", nil))
			if response != nil {
				t.Errorf("response = %#v, want nil", response)
			}
			if !errors.Is(err, ErrSDKResponseMalformed) {
				t.Fatalf("RoundTrip error = %v, want ErrSDKResponseMalformed", err)
			}
			if strings.Contains(err.Error(), "response-secret") {
				t.Errorf("error leaked response body: %v", err)
			}
			if !body.closed {
				t.Error("original response body was not closed")
			}
		})
	}
}

func TestBoundedSDKTransportPreservesBodyFailureCausesWithoutLeakingSensitiveText(t *testing.T) {
	const responseSecret = "response-secret"
	const credentialSecret = "credential-secret"
	readCause := errors.New("read failure containing " + responseSecret + " and " + credentialSecret)
	closeCause := errors.New("close failure containing " + responseSecret + " and " + credentialSecret)

	tests := []struct {
		name       string
		readErr    error
		closeErr   error
		wantCauses []error
	}{
		{
			name:       "read only",
			readErr:    readCause,
			wantCauses: []error{readCause},
		},
		{
			name:       "close only",
			closeErr:   closeCause,
			wantCauses: []error{closeCause},
		},
		{
			name:       "read and close",
			readErr:    readCause,
			closeErr:   closeCause,
			wantCauses: []error{readCause, closeCause},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := &transportTrackingReadCloser{
				Reader:   strings.NewReader(`{"ok":true}`),
				readErr:  test.readErr,
				closeErr: test.closeErr,
			}
			transport := NewBoundedSDKTransport(transportFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
			}), 1<<20)
			request := httptest.NewRequest(http.MethodGet, "https://example.test/releases", nil)
			request.Header.Set("Authorization", "Bearer "+credentialSecret)

			response, err := transport.RoundTrip(request)
			if response != nil {
				t.Errorf("response = %#v, want nil", response)
			}
			if err == nil {
				t.Fatal("RoundTrip error = nil, want body failure")
			}
			for _, cause := range test.wantCauses {
				if !errors.Is(err, cause) {
					t.Errorf("errors.Is(%v) = false, want true", cause)
				}
			}
			if strings.Contains(err.Error(), responseSecret) || strings.Contains(err.Error(), credentialSecret) {
				t.Errorf("error leaked sensitive data: %v", err)
			}
			if !body.closed {
				t.Error("original response body was not closed")
			}
		})
	}
}

func TestBoundedSDKTransportLeavesNon2xxBodyForSDKDecoding(t *testing.T) {
	const payload = "not JSON"
	body := &transportTrackingReadCloser{Reader: strings.NewReader(payload)}
	transport := NewBoundedSDKTransport(transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadRequest, Body: body}, nil
	}), int64(len(payload)))

	response, err := transport.RoundTrip(httptest.NewRequest(http.MethodGet, "https://example.test/releases", nil))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	got, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read restored body: %v", err)
	}
	if string(got) != payload {
		t.Errorf("restored body = %q, want %q", got, payload)
	}
	if !body.closed {
		t.Error("original response body was not closed")
	}
}

func TestBoundedSDKTransportUsesDefaultTransportWhenBaseNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	transport := NewBoundedSDKTransport(nil, 1<<20)
	request := httptest.NewRequest(http.MethodGet, server.URL, nil)
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (fn transportFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type transportTrackingReadCloser struct {
	io.Reader
	closed   bool
	read     int64
	readErr  error
	closeErr error
}

func (r *transportTrackingReadCloser) Read(p []byte) (int, error) {
	if r.readErr != nil {
		return 0, r.readErr
	}
	n, err := r.Reader.Read(p)
	r.read += int64(n)
	return n, err
}

func (r *transportTrackingReadCloser) Close() error {
	r.closed = true
	return r.closeErr
}
