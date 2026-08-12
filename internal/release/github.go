package release

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/go-github/v90/github"
)

const (
	defaultGitHubAPIBase = "https://api.github.com"
	maxGitHubRetryDelay  = time.Second
)

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

	// wait is replaceable only for hermetic retry tests.
	wait func(context.Context, time.Duration) error
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
		wait:             waitForGitHubRetry,
	}
}

// ReleaseByTag obtains the published release associated with tag.
func (c *GitHubReleaseClient) ReleaseByTag(ctx context.Context, tag string) (GitHubRelease, error) {
	client, err := c.sdkClient()
	if err != nil {
		return GitHubRelease{}, err
	}

	var result *github.RepositoryRelease
	err = c.retry(ctx, func(attemptContext context.Context) (*github.Response, error) {
		var response *github.Response
		result, response, err = client.Repositories.GetReleaseByTag(
			attemptContext,
			escapeGitHubPathSegment(c.Owner),
			escapeGitHubPathSegment(c.Repository),
			escapeGitHubPathSegment(tag),
		)
		return response, err
	})
	if err != nil {
		return GitHubRelease{}, err
	}
	if result == nil || result.GetID() <= 0 {
		return GitHubRelease{}, ErrGitHubRequest
	}
	return GitHubRelease{ID: result.GetID(), Body: result.GetBody()}, nil
}

// UpdateReleaseBody changes only a release's body; it never changes release
// metadata, publication state, or tag association.
func (c *GitHubReleaseClient) UpdateReleaseBody(ctx context.Context, id int64, body string) error {
	if id <= 0 {
		return errors.New("github release id must be positive")
	}
	client, err := c.sdkClient()
	if err != nil {
		return err
	}
	return c.retry(ctx, func(attemptContext context.Context) (*github.Response, error) {
		_, response, err := client.Repositories.UpdateRelease(
			attemptContext,
			escapeGitHubPathSegment(c.Owner),
			escapeGitHubPathSegment(c.Repository),
			id,
			github.UpdateReleaseRequest{Body: github.Ptr(body)},
		)
		return response, err
	})
}

func (c *GitHubReleaseClient) valid() error {
	if c == nil || c.Client == nil || c.APIBase == "" || c.Token == "" || c.Owner == "" || c.Repository == "" || c.RequestTimeout <= 0 || c.MaxResponseBytes <= 0 {
		return errors.New("github release client is not configured")
	}
	if _, err := normalizedGitHubAPIBase(c.APIBase); err != nil {
		return errors.New("github release client is not configured")
	}
	return nil
}

func (c *GitHubReleaseClient) sdkClient() (*github.Client, error) {
	if err := c.valid(); err != nil {
		return nil, err
	}
	base, err := normalizedGitHubAPIBase(c.APIBase)
	if err != nil {
		return nil, errors.New("github release client is not configured")
	}
	httpClient := c.sdkHTTPClient()
	client, err := github.NewClient(
		github.WithHTTPClient(httpClient),
		github.WithAuthToken(c.Token),
		github.WithTimeout(c.RequestTimeout),
		github.WithURLs(&base, nil),
		github.WithDisableRateLimitCheck(),
	)
	if err != nil {
		return nil, errors.New("github release client is not configured")
	}
	return client, nil
}

func (c *GitHubReleaseClient) sdkHTTPClient() *http.Client {
	if client, ok := c.Client.(*http.Client); ok {
		clone := *client
		clone.Transport = NewBoundedSDKTransport(clone.Transport, c.MaxResponseBytes)
		clone.CheckRedirect = rejectGitHubRedirect
		return &clone
	}
	return &http.Client{
		Transport:     NewBoundedSDKTransport(httpDoerTransport{doer: c.Client}, c.MaxResponseBytes),
		CheckRedirect: rejectGitHubRedirect,
	}
}

func rejectGitHubRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

func normalizedGitHubAPIBase(apiBase string) (string, error) {
	parsed, err := url.Parse(apiBase)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("invalid github api base")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/"
	return parsed.String(), nil
}

func escapeGitHubPathSegment(value string) string {
	return url.PathEscape(value)
}

func (c *GitHubReleaseClient) retry(ctx context.Context, operation func(context.Context) (*github.Response, error)) error {
	retries := min(max(c.MaxRetries, 0), 2)
	attempts := retries + 1
	for attempt := 0; attempt < attempts; attempt++ {
		attemptContext, cancel := context.WithTimeout(ctx, c.RequestTimeout)
		response, err := operation(attemptContext)
		cancel()
		if err == nil {
			return nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return safeGitHubError(err)
		}
		if isRetryableGitHubResponse(response) && attempt+1 < attempts {
			if err := c.retryWait(ctx, githubRetryDelay(attempt)); err != nil {
				return safeGitHubError(err)
			}
			continue
		}
		return safeGitHubError(err)
	}
	return ErrGitHubRequest
}

func isRetryableGitHubResponse(response *github.Response) bool {
	if response == nil || response.Response == nil {
		return false
	}
	status := response.StatusCode
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func githubRetryDelay(attempt int) time.Duration {
	delay := 100 * time.Millisecond * time.Duration(1<<attempt)
	if delay > maxGitHubRetryDelay {
		return maxGitHubRetryDelay
	}
	return delay
}

func (c *GitHubReleaseClient) retryWait(ctx context.Context, delay time.Duration) error {
	if c.wait == nil {
		return waitForGitHubRetry(ctx, delay)
	}
	return c.wait(ctx, delay)
}

func waitForGitHubRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func safeGitHubError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, ErrSDKResponseTooLarge) {
		return ErrGitHubResponseTooLarge
	}
	return ErrGitHubRequest
}
