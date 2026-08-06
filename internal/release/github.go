package release

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultGitHubAPIBase = "https://api.github.com"

// ErrGitHubRequest identifies a failed GitHub API request without exposing a
// token or an API response body.
var ErrGitHubRequest = errors.New("github release request failed")

// ErrGitHubResponseTooLarge identifies a response that exceeded the
// configured byte limit before JSON decoding or body draining.
var ErrGitHubResponseTooLarge = errors.New("github release response too large")

// GitHubRelease is the minimal release representation required for note updates.
type GitHubRelease struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

// GitHubReleaseClient reads and updates existing GitHub Releases.
type GitHubReleaseClient struct {
	Client     HTTPDoer
	APIBase    string
	Token      string
	Owner      string
	Repository string
	MaxRetries int
	// RequestTimeout bounds every attempt, including response-body reads.
	RequestTimeout   time.Duration
	MaxResponseBytes int64
}

// NewGitHubReleaseClient constructs a GitHub Releases client with bounded
// retries for rate-limit and server failures.
func NewGitHubReleaseClient(client HTTPDoer, apiBase, token, owner, repository string) *GitHubReleaseClient {
	if client == nil {
		client = &http.Client{Timeout: defaultRequestTimeout}
	}
	if apiBase == "" {
		apiBase = defaultGitHubAPIBase
	}
	return &GitHubReleaseClient{
		Client:           client,
		APIBase:          strings.TrimRight(apiBase, "/"),
		Token:            token,
		Owner:            owner,
		Repository:       repository,
		MaxRetries:       2,
		RequestTimeout:   defaultRequestTimeout,
		MaxResponseBytes: defaultMaxResponseBytes,
	}
}

// ReleaseByTag obtains the published release associated with tag.
func (c *GitHubReleaseClient) ReleaseByTag(ctx context.Context, tag string) (GitHubRelease, error) {
	if err := c.valid(); err != nil {
		return GitHubRelease{}, err
	}
	payload, err := c.request(ctx, http.MethodGet, "/repos/"+url.PathEscape(c.Owner)+"/"+url.PathEscape(c.Repository)+"/releases/tags/"+url.PathEscape(tag), nil)
	if err != nil {
		return GitHubRelease{}, err
	}
	var release GitHubRelease
	if err := decodeStrictJSON(payload, &release); err != nil {
		return GitHubRelease{}, fmt.Errorf("decode github release: %w", err)
	}
	if release.ID <= 0 {
		return GitHubRelease{}, errors.New("github release response has no id")
	}
	return release, nil
}

// UpdateReleaseBody changes only a release's body; it never changes release
// metadata, publication state, or tag association.
func (c *GitHubReleaseClient) UpdateReleaseBody(ctx context.Context, id int64, body string) error {
	if err := c.valid(); err != nil {
		return err
	}
	if id <= 0 {
		return errors.New("github release id must be positive")
	}
	payload, err := json.Marshal(struct {
		Body string `json:"body"`
	}{Body: body})
	if err != nil {
		return fmt.Errorf("encode github release patch: %w", err)
	}
	_, err = c.request(ctx, http.MethodPatch, "/repos/"+url.PathEscape(c.Owner)+"/"+url.PathEscape(c.Repository)+"/releases/"+fmt.Sprint(id), payload)
	if err != nil {
		return err
	}
	return nil
}

func (c *GitHubReleaseClient) valid() error {
	if c == nil || c.Client == nil || c.APIBase == "" || c.Token == "" || c.Owner == "" || c.Repository == "" || c.RequestTimeout <= 0 || c.MaxResponseBytes <= 0 {
		return errors.New("github release client is not configured")
	}
	return nil
}

func (c *GitHubReleaseClient) request(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	attempts := c.MaxRetries + 1
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 0; attempt < attempts; attempt++ {
		attemptContext, cancel := context.WithTimeout(ctx, c.RequestTimeout)
		req, err := http.NewRequestWithContext(attemptContext, method, c.APIBase+path, bytes.NewReader(body))
		if err != nil {
			cancel()
			return nil, fmt.Errorf("create github request: %w", err)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		response, err := c.Client.Do(req)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("github request: %w", err)
		}
		if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
			payload, oversize, readErr := readAllBounded(response.Body, c.MaxResponseBytes)
			response.Body.Close()
			cancel()
			if readErr != nil {
				return nil, fmt.Errorf("read github response: %w", readErr)
			}
			if oversize {
				return nil, fmt.Errorf("%w: limit %d bytes", ErrGitHubResponseTooLarge, c.MaxResponseBytes)
			}
			return payload, nil
		}
		retry := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError
		oversize, drainErr := drainBounded(response.Body, c.MaxResponseBytes)
		response.Body.Close()
		cancel()
		if drainErr != nil {
			return nil, fmt.Errorf("drain github response: %w", drainErr)
		}
		if oversize {
			return nil, fmt.Errorf("%w: limit %d bytes", ErrGitHubResponseTooLarge, c.MaxResponseBytes)
		}
		if retry && attempt+1 < attempts {
			continue
		}
		return nil, fmt.Errorf("%w: status %d", ErrGitHubRequest, response.StatusCode)
	}
	return nil, ErrGitHubRequest
}
