package thresh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fetchCall struct {
	source string
	since  time.Time
	until  time.Time
}

type fakeFeedResponse struct {
	items []FeedItem
	err   error
}

type fakeFeedFetcher struct {
	responses map[string]fakeFeedResponse
	calls     []fetchCall
}

func (f *fakeFeedFetcher) Fetch(
	_ context.Context,
	source string,
	since, until time.Time,
) ([]FeedItem, error) {
	f.calls = append(f.calls, fetchCall{source: source, since: since, until: until})
	response := f.responses[source]
	return response.items, response.err
}

type mdhqCall struct {
	url     string
	options MDHQOptions
}

type fakeMDHQResponse struct {
	result MDHQResult
	err    error
}

type fakeMDHQ struct {
	responses map[string]fakeMDHQResponse
	calls     []mdhqCall
	afterGet  func()
}

func (m *fakeMDHQ) Get(
	_ context.Context,
	url string,
	options MDHQOptions,
) (MDHQResult, error) {
	m.calls = append(m.calls, mdhqCall{url: url, options: options})
	response := m.responses[url]
	if m.afterGet != nil {
		m.afterGet()
	}
	return response.result, response.err
}

func TestCollectFeedsPreservesOrderDeduplicatesAndContinues(t *testing.T) {
	t.Parallel()

	since := time.Date(2026, 9, 10, 7, 0, 0, 0, time.UTC)
	until := time.Date(2026, 9, 11, 7, 0, 0, 0, time.UTC)
	fetcher := &fakeFeedFetcher{
		responses: map[string]fakeFeedResponse{
			"feed-a": {
				items: []FeedItem{
					{URL: "https://example.com/1"},
					{},
					{URL: "https://example.com/shared"},
					{URL: "https://example.com/1"},
				},
			},
			"feed-b": {err: errors.New("feed unavailable")},
			"feed-c": {
				items: []FeedItem{
					{URL: "https://example.com/shared"},
					{URL: "https://example.com/2"},
				},
			},
		},
	}

	got, err := CollectFeeds(
		context.Background(),
		fetcher,
		[]string{"feed-a", "feed-b", "feed-c"},
		since,
		until,
	)
	if err == nil || !strings.Contains(err.Error(), `fetch source "feed-b": feed unavailable`) {
		t.Fatalf("CollectFeeds error = %v", err)
	}
	want := []CollectedURL{
		{URL: "https://example.com/1", SourceURL: "feed-a"},
		{URL: "https://example.com/shared", SourceURL: "feed-a"},
		{URL: "https://example.com/2", SourceURL: "feed-c"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CollectFeeds URLs = %#v, want %#v", got, want)
	}
	wantCalls := []fetchCall{
		{source: "feed-a", since: since, until: until},
		{source: "feed-b", since: since, until: until},
		{source: "feed-c", since: since, until: until},
	}
	if !reflect.DeepEqual(fetcher.calls, wantCalls) {
		t.Fatalf("fetch calls = %#v, want %#v", fetcher.calls, wantCalls)
	}
}

func TestPipelineRunPreservesOrderAndAggregatesPartialFailures(t *testing.T) {
	t.Parallel()

	fetcher := &fakeFeedFetcher{
		responses: map[string]fakeFeedResponse{
			"feed-a": {
				items: []FeedItem{
					{URL: "https://example.com/1"},
					{URL: "https://example.com/2"},
				},
			},
			"feed-b": {err: errors.New("fetch failed")},
			"feed-c": {
				items: []FeedItem{{URL: "https://example.com/3"}},
			},
		},
	}
	mdhq := &fakeMDHQ{
		responses: map[string]fakeMDHQResponse{
			"https://example.com/1": {
				result: MDHQResult{
					RequestedURL: "https://example.com/1",
					SourceURL:    "https://example.com/final-1",
					Path:         "/root/1.md",
					Status:       "saved",
					Diagnostic:   "asset warning",
				},
			},
			"https://example.com/2": {err: errors.New("mdhq failed")},
			"https://example.com/3": {
				result: MDHQResult{
					RequestedURL: "https://example.com/3",
					SourceURL:    "https://example.com/3",
					Path:         "/root/3.md",
					Status:       "unchanged",
				},
			},
		},
	}
	pipeline := NewPipeline(fetcher, mdhq)
	var stdout, stderr bytes.Buffer
	err := pipeline.Run(context.Background(), PipelineRequest{
		Sources: []string{"feed-a", "feed-b", "feed-c"},
		Root:    "/root",
		Assets:  true,
		Update:  true,
	}, &stdout, &stderr)

	if err == nil {
		t.Fatal("Pipeline.Run error = nil")
	}
	for _, message := range []string{
		`fetch source "feed-b": fetch failed`,
		`process URL "https://example.com/2" from source "feed-a": mdhq failed`,
		`mdhq "https://example.com/1": asset warning`,
	} {
		if strings.HasPrefix(message, "mdhq ") {
			if !strings.Contains(stderr.String(), message) {
				t.Errorf("stderr %q does not contain %q", stderr.String(), message)
			}
			continue
		}
		if !strings.Contains(err.Error(), message) {
			t.Errorf("Pipeline.Run error %q does not contain %q", err, message)
		}
		if !strings.Contains(stderr.String(), message) {
			t.Errorf("stderr %q does not contain %q", stderr.String(), message)
		}
	}

	wantOutput := "" +
		"{\"requestedUrl\":\"https://example.com/1\",\"sourceUrl\":\"https://example.com/final-1\",\"path\":\"/root/1.md\",\"status\":\"saved\"}\n" +
		"{\"requestedUrl\":\"https://example.com/3\",\"sourceUrl\":\"https://example.com/3\",\"path\":\"/root/3.md\",\"status\":\"unchanged\"}\n"
	if stdout.String() != wantOutput {
		t.Fatalf("stdout = %q, want %q", stdout.String(), wantOutput)
	}
	wantCalls := []mdhqCall{
		{
			url: "https://example.com/1",
			options: MDHQOptions{
				Root:   "/root",
				Assets: true,
				Update: true,
			},
		},
		{
			url: "https://example.com/2",
			options: MDHQOptions{
				Root:   "/root",
				Assets: true,
				Update: true,
			},
		},
		{
			url: "https://example.com/3",
			options: MDHQOptions{
				Root:   "/root",
				Assets: true,
				Update: true,
			},
		},
	}
	if !reflect.DeepEqual(mdhq.calls, wantCalls) {
		t.Fatalf("mdhq calls = %#v, want %#v", mdhq.calls, wantCalls)
	}
}

func TestCollectFeedsRejectsNonHTTPArticleURLs(t *testing.T) {
	t.Parallel()

	fetcher := &fakeFeedFetcher{
		responses: map[string]fakeFeedResponse{
			"feed-a": {
				items: []FeedItem{
					{URL: "--root=/tmp/evil"},
					{URL: "mailto:someone@example.com"},
					{URL: "/relative/path"},
					{URL: "https://user:pass@example.com/secret"},
					{URL: "--root=/tmp/evil"},
					{URL: "https://example.com/ok"},
				},
			},
		},
	}
	credentialUsername := "private-login"
	credentialPassword := "private-password"
	response := fetcher.responses["feed-a"]
	response.items[3].URL = "https://" + credentialUsername + ":" +
		credentialPassword + "@example.com/secret"
	response.items = append(response.items, FeedItem{URL: "HTTPS://example.com/uppercase"})
	fetcher.responses["feed-a"] = response

	got, err := CollectFeeds(
		context.Background(),
		fetcher,
		[]string{"feed-a"},
		time.Time{},
		time.Time{},
	)
	want := []CollectedURL{
		{URL: "https://example.com/ok", SourceURL: "feed-a"},
		{URL: "HTTPS://example.com/uppercase", SourceURL: "feed-a"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CollectFeeds URLs = %#v, want %#v", got, want)
	}
	if err == nil {
		t.Fatal("CollectFeeds error = nil, want rejected URLs")
	}
	for _, message := range []string{
		`skip item from source "feed-a": url "--root=/tmp/evil" must use http or https`,
		`skip item from source "feed-a": url "mailto:someone@example.com" must use http or https`,
		`skip item from source "feed-a": url "/relative/path" must use http or https`,
		`skip item from source "feed-a": url "https://user:pass@example.com/secret" must not contain userinfo`,
	} {
		if strings.HasSuffix(message, "must not contain userinfo") {
			continue
		}
		if !strings.Contains(err.Error(), message) {
			t.Errorf("CollectFeeds error %q does not contain %q", err, message)
		}
	}
	if !strings.Contains(
		err.Error(),
		`skip item from source "feed-a": url "https://example.com/secret" must not contain userinfo`,
	) {
		t.Errorf("CollectFeeds error %q does not retain safe URL context", err)
	}
	for _, credential := range []string{credentialUsername, credentialPassword} {
		if strings.Contains(err.Error(), credential) {
			t.Errorf("CollectFeeds error %q contains credential %q", err, credential)
		}
	}
	if got := strings.Count(err.Error(), "--root=/tmp/evil"); got != 1 {
		t.Errorf("duplicate invalid URL reported %d times, want 1", got)
	}
}

func TestMDHQGetterContractRejectsNonHTTPURL(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{}
	_, err := NewMDHQ(runner).Get(
		context.Background(),
		"--root=/tmp/evil",
		MDHQOptions{Root: "/root"},
	)
	if err == nil || !strings.Contains(err.Error(), "must use http or https") {
		t.Fatalf("MDHQ.Get error = %v, want a rejected URL", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %#v, want none", runner.calls)
	}
}

func TestCollectFeedsStopsWhenContextIsCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fetcher := &fakeFeedFetcher{responses: map[string]fakeFeedResponse{}}
	_, err := CollectFeeds(ctx, fetcher, []string{"feed-a", "feed-b"}, time.Time{}, time.Time{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CollectFeeds error = %v, want context.Canceled", err)
	}
	if len(fetcher.calls) != 0 {
		t.Fatalf("fetch calls = %#v, want none", fetcher.calls)
	}
}

func TestCollectFeedsPreservesFetchFailureOnCancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ctxErr   error
		fetchErr error
	}{
		{
			name:     "canceled",
			ctxErr:   context.Canceled,
			fetchErr: errors.New("feed transport failed"),
		},
		{
			name:     "deadline exceeded",
			ctxErr:   context.DeadlineExceeded,
			fetchErr: errors.New("feed process killed"),
		},
		{
			name:     "fetch error already wraps cancellation",
			ctxErr:   context.Canceled,
			fetchErr: fmt.Errorf("feed transport failed: %w", context.Canceled),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := &mutableErrorContext{Context: context.Background()}
			fetcher := feedFetcherFunc(func(
				_ context.Context,
				source string,
				_, _ time.Time,
			) ([]FeedItem, error) {
				if source == "feed-a" {
					return []FeedItem{{URL: "https://example.com/1"}}, nil
				}
				ctx.err = tt.ctxErr
				return nil, tt.fetchErr
			})

			got, err := CollectFeeds(
				ctx,
				fetcher,
				[]string{"feed-a", "feed-b", "feed-c"},
				time.Time{},
				time.Time{},
			)

			if !errors.Is(err, tt.ctxErr) {
				t.Fatalf("CollectFeeds error = %v, want %v", err, tt.ctxErr)
			}
			if !errors.Is(err, tt.fetchErr) {
				t.Fatalf("CollectFeeds error = %v, want fetch error %v", err, tt.fetchErr)
			}
			if got, want := strings.Count(err.Error(), tt.ctxErr.Error()), 1; got != want {
				t.Fatalf("CollectFeeds error = %q, context count = %d, want %d", err, got, want)
			}
			want := []CollectedURL{{URL: "https://example.com/1", SourceURL: "feed-a"}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("CollectFeeds URLs = %#v, want %#v", got, want)
			}
		})
	}
}

func TestCollectFeedsFetchFailurePreservesCancellationWithoutURLs(t *testing.T) {
	t.Parallel()

	ctx := &mutableErrorContext{Context: context.Background()}
	fetchErr := errors.New("feed transport failed")
	fetcher := feedFetcherFunc(func(
		_ context.Context,
		_ string,
		_, _ time.Time,
	) ([]FeedItem, error) {
		ctx.err = context.Canceled
		return nil, fetchErr
	})

	got, err := CollectFeeds(
		ctx,
		fetcher,
		[]string{"feed-a"},
		time.Time{},
		time.Time{},
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CollectFeeds error = %v, want context.Canceled", err)
	}
	if !errors.Is(err, fetchErr) {
		t.Fatalf("CollectFeeds error = %v, want fetch error %v", err, fetchErr)
	}
	if got != nil {
		t.Fatalf("CollectFeeds URLs = %#v, want nil", got)
	}
}

func TestPipelineRunReportsCancellationBetweenArticlesOnce(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	fetcher := &fakeFeedFetcher{
		responses: map[string]fakeFeedResponse{
			"feed-a": {
				items: []FeedItem{
					{URL: "https://example.com/1"},
					{URL: "https://example.com/2"},
				},
			},
		},
	}
	mdhq := &fakeMDHQ{
		responses: map[string]fakeMDHQResponse{
			"https://example.com/1": {
				result: MDHQResult{
					RequestedURL: "https://example.com/1",
					SourceURL:    "https://example.com/1",
					Path:         "/root/1.md",
					Status:       "saved",
				},
			},
		},
		afterGet: cancel,
	}
	var stdout, stderr bytes.Buffer
	err := NewPipeline(fetcher, mdhq).Run(
		ctx,
		PipelineRequest{Sources: []string{"feed-a"}},
		&stdout,
		&stderr,
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Pipeline.Run error = %v, want context.Canceled", err)
	}
	if got, want := stderr.String(), "context canceled\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
	if got, want := len(mdhq.calls), 1; got != want {
		t.Fatalf("mdhq calls = %d, want %d", got, want)
	}
}

func TestPipelineRunReportsFetchFailureAndCancellationOnce(t *testing.T) {
	t.Parallel()

	ctx := &mutableErrorContext{Context: context.Background()}
	fetchErr := errors.New("feed transport failed")
	fetcher := feedFetcherFunc(func(
		_ context.Context,
		source string,
		_, _ time.Time,
	) ([]FeedItem, error) {
		if source == "feed-a" {
			return []FeedItem{{URL: "https://example.com/1"}}, nil
		}
		ctx.err = context.Canceled
		return nil, fetchErr
	})
	mdhq := &fakeMDHQ{responses: map[string]fakeMDHQResponse{}}
	var stdout, stderr bytes.Buffer
	err := NewPipeline(fetcher, mdhq).Run(
		ctx,
		PipelineRequest{Sources: []string{"feed-a", "feed-b", "feed-c"}},
		&stdout,
		&stderr,
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Pipeline.Run error = %v, want context.Canceled", err)
	}
	if !errors.Is(err, fetchErr) {
		t.Fatalf("Pipeline.Run error = %v, want fetch error %v", err, fetchErr)
	}
	if got, want := strings.Count(stderr.String(), context.Canceled.Error()), 1; got != want {
		t.Fatalf("stderr = %q, cancellation count = %d, want %d", stderr.String(), got, want)
	}
	if got, want := strings.Count(stderr.String(), fetchErr.Error()), 1; got != want {
		t.Fatalf("stderr = %q, fetch failure count = %d, want %d", stderr.String(), got, want)
	}
	if len(mdhq.calls) != 0 {
		t.Fatalf("mdhq calls = %#v, want none", mdhq.calls)
	}
}

func TestPipelineRunPreservesMDHQFailureOnCancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		items      []FeedItem
		processErr error
	}{
		{
			name:       "final URL",
			items:      []FeedItem{{URL: "https://example.com/1"}},
			processErr: errors.New("mdhq process killed"),
		},
		{
			name: "remaining URLs",
			items: []FeedItem{
				{URL: "https://example.com/1"},
				{URL: "https://example.com/2"},
				{URL: "https://example.com/3"},
			},
			processErr: errors.New("mdhq process killed"),
		},
		{
			name:       "process error already wraps cancellation",
			items:      []FeedItem{{URL: "https://example.com/1"}},
			processErr: fmt.Errorf("mdhq process killed: %w", context.Canceled),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			fetcher := &fakeFeedFetcher{
				responses: map[string]fakeFeedResponse{
					"feed-a": {items: tt.items},
				},
			}
			mdhq := &fakeMDHQ{
				responses: map[string]fakeMDHQResponse{
					"https://example.com/1": {err: tt.processErr},
				},
				afterGet: cancel,
			}
			var stdout, stderr bytes.Buffer
			err := NewPipeline(fetcher, mdhq).Run(
				ctx,
				PipelineRequest{Sources: []string{"feed-a"}},
				&stdout,
				&stderr,
			)

			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Pipeline.Run error = %v, want context.Canceled", err)
			}
			if !errors.Is(err, tt.processErr) {
				t.Fatalf("Pipeline.Run error = %v, want process error %v", err, tt.processErr)
			}
			if !strings.Contains(err.Error(), "mdhq process killed") {
				t.Fatalf("Pipeline.Run error = %v, want process failure", err)
			}
			if got, want := strings.Count(stderr.String(), "context canceled"), 1; got != want {
				t.Fatalf("stderr = %q, cancellation count = %d, want %d", stderr.String(), got, want)
			}
			if got, want := strings.Count(stderr.String(), "mdhq process killed"), 1; got != want {
				t.Fatalf("stderr = %q, process failure count = %d, want %d", stderr.String(), got, want)
			}
			if got, want := len(mdhq.calls), 1; got != want {
				t.Fatalf("mdhq calls = %d, want %d", got, want)
			}
		})
	}
}

func TestPipelineRunDoesNotRepeatCancellationLoggedDuringCollection(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	fetcher := feedFetcherFunc(func(
		_ context.Context,
		source string,
		_, _ time.Time,
	) ([]FeedItem, error) {
		if source == "feed-a" {
			cancel()
			return []FeedItem{{URL: "https://example.com/1"}}, nil
		}
		t.Fatalf("Fetch called for %q after cancellation", source)
		return nil, nil
	})
	mdhq := &fakeMDHQ{responses: map[string]fakeMDHQResponse{}}
	var stdout, stderr bytes.Buffer
	err := NewPipeline(fetcher, mdhq).Run(
		ctx,
		PipelineRequest{Sources: []string{"feed-a", "feed-b"}},
		&stdout,
		&stderr,
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Pipeline.Run error = %v, want context.Canceled", err)
	}
	if got, want := stderr.String(), "context canceled\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
	if len(mdhq.calls) != 0 {
		t.Fatalf("mdhq calls = %#v, want none", mdhq.calls)
	}
}

type feedFetcherFunc func(
	context.Context,
	string,
	time.Time,
	time.Time,
) ([]FeedItem, error)

type mutableErrorContext struct {
	context.Context
	err error
}

func (c *mutableErrorContext) Err() error {
	return c.err
}

func (f feedFetcherFunc) Fetch(
	ctx context.Context,
	source string,
	since, until time.Time,
) ([]FeedItem, error) {
	return f(ctx, source, since, until)
}
