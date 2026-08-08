package release

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGitHubReleaseClientLooksUpTagAndPatchesBodyOnly(t *testing.T) {
	var patch map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer github-token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github.v3+json" {
			t.Errorf("Accept = %q", got)
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
			t.Errorf("X-GitHub-Api-Version = %q", got)
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/acme/emberfall/releases/tags/v1.2.3":
			_, _ = w.Write([]byte(`{"id":42,"tag_name":"v1.2.3","body":"# Deterministic"}`))
		case "PATCH /repos/acme/emberfall/releases/42":
			if got := r.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("PATCH Content-Type = %q", got)
			}
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"id":42,"body":"# Curated"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewGitHubReleaseClient(server.Client(), server.URL, "github-token", "acme", "emberfall")
	release, err := client.ReleaseByTag(context.Background(), "v1.2.3")
	if err != nil {
		t.Fatalf("ReleaseByTag: %v", err)
	}
	if release.ID != 42 || release.Body != "# Deterministic" {
		t.Fatalf("release = %#v", release)
	}
	if err := client.UpdateReleaseBody(context.Background(), release.ID, "# Curated"); err != nil {
		t.Fatalf("UpdateReleaseBody: %v", err)
	}
	if len(patch) != 1 || patch["body"] != "# Curated" {
		t.Errorf("patch body = %#v, want only body", patch)
	}
}

func TestGitHubReleaseClientEscapesEveryPathSegment(t *testing.T) {
	var escapedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		escapedPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{"id":42,"body":"# Deterministic"}`))
	}))
	defer server.Close()
	client := NewGitHubReleaseClient(server.Client(), server.URL, "github-token", "acme/team", "ember fall")
	if _, err := client.ReleaseByTag(context.Background(), "v1.2.3/rc 1"); err != nil {
		t.Fatalf("ReleaseByTag: %v", err)
	}
	if escapedPath != "/repos/acme%2Fteam/ember%20fall/releases/tags/v1.2.3%2Frc%201" {
		t.Fatalf("escaped path = %q", escapedPath)
	}
}

func TestGitHubReleaseClientRetriesRateLimits(t *testing.T) {
	attempts := 0
	firstBody := &countingReadCloser{remaining: len("try later")}
	client := NewGitHubReleaseClient(roundTripperFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return &http.Response{StatusCode: http.StatusTooManyRequests, Body: firstBody}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":42,"body":"# Deterministic"}`))}, nil
	}), "https://api.example.test", "github-token", "acme", "emberfall")
	client.MaxRetries = 1
	if _, err := client.ReleaseByTag(context.Background(), "v1.2.3"); err != nil || attempts != 2 {
		t.Fatalf("ReleaseByTag = %v after %d attempts", err, attempts)
	}
	if firstBody.read != len("try later") || !firstBody.closed {
		t.Fatalf("retry body read/closed = %d/%t", firstBody.read, firstBody.closed)
	}
}

func TestGitHubReleaseClientUsesContextAwareRetryWaiter(t *testing.T) {
	client := NewGitHubReleaseClient(roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(strings.NewReader("retry"))}, nil
	}), "https://api.example.test", "github-token", "acme", "emberfall")
	client.MaxRetries = 1
	waits := 0
	client.wait = func(ctx context.Context, delay time.Duration) error {
		waits++
		if delay <= 0 {
			t.Fatalf("retry delay = %s, want positive", delay)
		}
		return nil
	}
	_, err := client.ReleaseByTag(context.Background(), "v1.2.3")
	if !errors.Is(err, ErrGitHubRequest) || waits != 1 {
		t.Fatalf("ReleaseByTag = %v; waits = %d", err, waits)
	}
}

func TestGitHubReleaseClientEnforcesRequestTimeoutWithInjectedClient(t *testing.T) {
	client := NewGitHubReleaseClient(roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if _, ok := request.Context().Deadline(); !ok {
			return nil, errors.New("request context has no deadline")
		}
		<-request.Context().Done()
		return nil, request.Context().Err()
	}), "https://api.example.test", "github-token", "acme", "emberfall")
	client.RequestTimeout = 5 * time.Millisecond
	_, err := client.ReleaseByTag(context.Background(), "v1.2.3")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ReleaseByTag error = %v, want deadline exceeded", err)
	}
}

func TestGitHubReleaseClientRejectsOversizeResponsesWithBoundedDrain(t *testing.T) {
	body := &countingReadCloser{remaining: 1_000}
	client := NewGitHubReleaseClient(roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusTooManyRequests, Body: body}, nil
	}), "https://api.example.test", "github-token", "acme", "emberfall")
	client.MaxResponseBytes = 32
	client.MaxRetries = 2
	_, err := client.ReleaseByTag(context.Background(), "v1.2.3")
	if !errors.Is(err, ErrGitHubResponseTooLarge) {
		t.Fatalf("ReleaseByTag error = %v, want oversize classification", err)
	}
	if body.read > 33 || !body.closed {
		t.Fatalf("error body read/closed = %d/%t, want at most 33/true", body.read, body.closed)
	}
}

func TestGitHubReleaseClientRejectsOversizeSuccessResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":42,"body":"` + strings.Repeat("x", 256) + `"}`))
	}))
	defer server.Close()
	client := NewGitHubReleaseClient(server.Client(), server.URL, "github-token", "acme", "emberfall")
	client.MaxResponseBytes = 64
	_, err := client.ReleaseByTag(context.Background(), "v1.2.3")
	if !errors.Is(err, ErrGitHubResponseTooLarge) {
		t.Fatalf("ReleaseByTag error = %v, want oversize classification", err)
	}
}

func TestGitHubReleaseClientHonorsEarlierCallerDeadline(t *testing.T) {
	client := NewGitHubReleaseClient(roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	}), "https://api.example.test", "github-token", "acme", "emberfall")
	client.RequestTimeout = time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err := client.ReleaseByTag(ctx, "v1.2.3")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ReleaseByTag error = %v, want caller deadline", err)
	}
}

func TestGitHubReleaseClientRejectsTrailingJSONData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":42,"body":"# Deterministic"} {"extra":true}`))
	}))
	defer server.Close()
	client := NewGitHubReleaseClient(server.Client(), server.URL, "github-token", "acme", "emberfall")
	if _, err := client.ReleaseByTag(context.Background(), "v1.2.3"); err == nil {
		t.Fatal("ReleaseByTag accepted trailing JSON data")
	}
}

func TestGitHubReleaseClientRetriesRetryableFailuresAndClassifiesErrors(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"id":42,"body":"# Deterministic"}`))
	}))
	defer server.Close()
	client := NewGitHubReleaseClient(server.Client(), server.URL, "github-token", "acme", "emberfall")
	client.MaxRetries = 2
	if _, err := client.ReleaseByTag(context.Background(), "v1.2.3"); err != nil || attempts != 3 {
		t.Fatalf("ReleaseByTag = %v after %d attempts", err, attempts)
	}

	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("github-token"))
	})
	_, err := client.ReleaseByTag(context.Background(), "v1.2.3")
	if err == nil || !errors.Is(err, ErrGitHubRequest) || strings.Contains(err.Error(), "github-token") {
		t.Fatalf("error = %v", err)
	}
}

func TestGitHubReleaseClientUsesExactCustomAPIBase(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{"id":42,"body":"# Deterministic"}`))
	}))
	defer server.Close()

	client := NewGitHubReleaseClient(server.Client(), server.URL+"/api/v3", "github-token", "acme", "emberfall")
	if _, err := client.ReleaseByTag(context.Background(), "v1.2.3"); err != nil {
		t.Fatalf("ReleaseByTag: %v", err)
	}
	if path != "/api/v3/repos/acme/emberfall/releases/tags/v1.2.3" {
		t.Fatalf("request path = %q", path)
	}
}

func TestGitHubReleaseClientRejectsRedirectsWithoutForwardingCredentials(t *testing.T) {
	targetRequests := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests++
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("redirect target Authorization = %q", got)
		}
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/stolen", http.StatusFound)
	}))
	defer source.Close()

	client := NewGitHubReleaseClient(source.Client(), source.URL, "github-token", "acme", "emberfall")
	_, err := client.ReleaseByTag(context.Background(), "v1.2.3")
	if !errors.Is(err, ErrGitHubRequest) || targetRequests != 0 {
		t.Fatalf("ReleaseByTag = %v; redirected requests = %d", err, targetRequests)
	}
}

func TestGitHubReleaseClientRejectsEmptyAndMalformedSuccessJSON(t *testing.T) {
	for name, body := range map[string]string{
		"empty":     "",
		"malformed": `{"id":`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			client := NewGitHubReleaseClient(server.Client(), server.URL, "github-token", "acme", "emberfall")
			_, err := client.ReleaseByTag(context.Background(), "v1.2.3")
			if !errors.Is(err, ErrGitHubRequest) {
				t.Fatalf("ReleaseByTag error = %v, want safe request error", err)
			}
		})
	}
}

func TestGitHubReleaseClientRetriesExactlyThreeOnlyForRetryableStatuses(t *testing.T) {
	for name, status := range map[string]int{
		"rate-limit": http.StatusTooManyRequests,
		"server":     http.StatusBadGateway,
	} {
		t.Run(name, func(t *testing.T) {
			attempts := 0
			client := NewGitHubReleaseClient(roundTripperFunc(func(*http.Request) (*http.Response, error) {
				attempts++
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("provider detail"))}, nil
			}), "https://api.example.test", "github-token", "acme", "emberfall")
			client.wait = func(context.Context, time.Duration) error { return nil }
			_, err := client.ReleaseByTag(context.Background(), "v1.2.3")
			if !errors.Is(err, ErrGitHubRequest) || attempts != 3 {
				t.Fatalf("ReleaseByTag = %v after %d attempts", err, attempts)
			}
		})
	}

	attempts := 0
	client := NewGitHubReleaseClient(roundTripperFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader("github-token"))}, nil
	}), "https://api.example.test", "github-token", "acme", "emberfall")
	client.wait = func(context.Context, time.Duration) error {
		t.Fatal("ordinary client error should not retry")
		return nil
	}
	_, err := client.ReleaseByTag(context.Background(), "v1.2.3")
	if !errors.Is(err, ErrGitHubRequest) || attempts != 1 || strings.Contains(err.Error(), "github-token") {
		t.Fatalf("ReleaseByTag = %v after %d attempts", err, attempts)
	}
}

func TestGitHubReleaseClientStopsRetryWaitWhenContextIsCanceled(t *testing.T) {
	attempts := 0
	client := NewGitHubReleaseClient(roundTripperFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("retry"))}, nil
	}), "https://api.example.test", "github-token", "acme", "emberfall")
	client.wait = func(ctx context.Context, _ time.Duration) error {
		<-ctx.Done()
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.ReleaseByTag(ctx, "v1.2.3")
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("ReleaseByTag = %v after %d attempts", err, attempts)
	}
}

func TestReleaseNotesUpdaterDoesNotPatchWhenEnhancementFails(t *testing.T) {
	patches := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patches++
		}
		_, _ = w.Write([]byte(`{"id":42,"body":"# Deterministic"}`))
	}))
	defer server.Close()
	client := NewGitHubReleaseClient(server.Client(), server.URL, "github-token", "acme", "emberfall")
	updater := ReleaseNotesUpdater{Releases: client, Enhancer: failingEnhancer{}}
	notes := "# Deterministic"
	got, err := updater.EnhanceRelease(context.Background(), EnhancementInput{Tag: "v1.2.3", Notes: notes})
	if err == nil || got != notes || patches != 0 {
		t.Fatalf("EnhanceRelease = %q, %v; patches = %d", got, err, patches)
	}
}

func TestReleaseNotesUpdaterSkipsEnhancedReleaseOnRerun(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet {
			t.Fatalf("request method = %s, want GET", r.Method)
		}
		_, _ = w.Write([]byte(`{"id":42,"body":"# Curated\n\n<!-- emberfall-claude-notes:v1 -->"}`))
	}))
	defer server.Close()
	client := NewGitHubReleaseClient(server.Client(), server.URL, "github-token", "acme", "emberfall")
	updater := ReleaseNotesUpdater{Releases: client, Enhancer: failingEnhancer{}}
	got, err := updater.EnhanceRelease(context.Background(), EnhancementInput{Tag: "v1.2.3", Notes: "# Deterministic"})
	if err != nil || !strings.Contains(got, notesEnhancementMarker) || requests != 1 {
		t.Fatalf("EnhanceRelease = %q, %v; requests = %d", got, err, requests)
	}
}

func TestReleaseNotesUpdaterUsesNarrowReleaseClientInterface(t *testing.T) {
	client := &recordingReleaseClient{release: GitHubRelease{ID: 42, Body: "# Deterministic"}}
	updater := ReleaseNotesUpdater{Releases: client, Enhancer: staticEnhancer("# Curated")}
	got, err := updater.EnhanceRelease(context.Background(), EnhancementInput{Tag: "v1.2.3", Notes: "# Deterministic"})
	if err != nil || got != "# Curated" || client.updatedID != 42 || client.updatedBody != "# Curated" {
		t.Fatalf("EnhanceRelease = %q, %v; client = %#v", got, err, client)
	}
}

type failingEnhancer struct{}

func (failingEnhancer) Enhance(context.Context, EnhancementInput) (string, error) {
	return "", errors.New("anthropic unavailable")
}

type staticEnhancer string

func (e staticEnhancer) Enhance(context.Context, EnhancementInput) (string, error) {
	return string(e), nil
}

type recordingReleaseClient struct {
	release     GitHubRelease
	updatedID   int64
	updatedBody string
}

func (c *recordingReleaseClient) ReleaseByTag(context.Context, string) (GitHubRelease, error) {
	return c.release, nil
}

func (c *recordingReleaseClient) UpdateReleaseBody(_ context.Context, id int64, body string) error {
	c.updatedID = id
	c.updatedBody = body
	return nil
}

type countingReadCloser struct {
	remaining int
	read      int
	closed    bool
}

func (r *countingReadCloser) Read(buffer []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	count := len(buffer)
	if count > r.remaining {
		count = r.remaining
	}
	for i := 0; i < count; i++ {
		buffer[i] = 'x'
	}
	r.remaining -= count
	r.read += count
	return count, nil
}

func (r *countingReadCloser) Close() error {
	r.closed = true
	return nil
}
