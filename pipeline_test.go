package metabol

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRSSnipFetcherNormalizesOnlySourceURLScheme(t *testing.T) {
	feed := readFixture(t, "rss", "source-url.xml")
	const requestURI = "/Feed.XML?Token=AbC%2FDef"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.RequestURI != requestURI {
			t.Errorf("request URI = %q, want %q", r.RequestURI, requestURI)
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(feed)
	}))
	defer server.Close()

	sourceURL := "HtTp" + strings.TrimPrefix(server.URL, "http") + requestURI
	items, err := (RSSnipFetcher{}).Fetch(
		context.Background(),
		sourceURL,
		time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("Fetch(%q) returned error: %v", sourceURL, err)
	}
	want := []FeedItem{{URL: "https://example.com/item"}}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("Fetch(%q) items = %#v, want %#v", sourceURL, items, want)
	}
}

func TestRSSnipFetcherRejectsUnparseableSourceURLSafely(t *testing.T) {
	sourceURL := "HTTPS://alice:swordfish@example.com/%zz"
	_, err := (RSSnipFetcher{}).Fetch(
		context.Background(),
		sourceURL,
		time.Time{},
		time.Time{},
	)
	if err == nil {
		t.Fatal("Fetch error = nil, want invalid URL error")
	}
	if got := err.Error(); !strings.Contains(got, `invalid url "HTTPS://example.com/%zz"`) {
		t.Errorf("Fetch error = %q, want safe URL context", got)
	}
	for _, secret := range []string{"alice", "swordfish"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("Fetch error %q contains credential %q", err, secret)
		}
	}
}

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

type partialErrorWriter struct {
	buffer bytes.Buffer
	err    error
}

func (w *partialErrorWriter) Write(p []byte) (int, error) {
	n := min(8, len(p))
	_, _ = w.buffer.Write(p[:n])
	return n, w.err
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

func TestPipelineRunStopsAfterOutputFailure(t *testing.T) {
	t.Parallel()

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
			"https://example.com/2": {
				result: MDHQResult{
					RequestedURL: "https://example.com/2",
					SourceURL:    "https://example.com/2",
					Path:         "/root/2.md",
					Status:       "saved",
				},
			},
		},
	}
	writeErr := errors.New("disk full")
	stdout := &partialErrorWriter{err: writeErr}
	var stderr bytes.Buffer
	err := NewPipeline(fetcher, mdhq).Run(
		context.Background(),
		PipelineRequest{Sources: []string{"feed-a"}, Root: "/root"},
		stdout,
		&stderr,
	)

	if !errors.Is(err, writeErr) {
		t.Fatalf("Pipeline.Run error = %v, want %v", err, writeErr)
	}
	if got, want := len(mdhq.calls), 1; got != want {
		t.Fatalf("mdhq calls = %d, want %d", got, want)
	}
	if got := stdout.buffer.String(); got == "" || strings.Contains(got, "https://example.com/2") {
		t.Fatalf("stdout = %q, want only a partial first record", got)
	}
	if got := stderr.String(); !strings.Contains(got, `write result for URL "https://example.com/1"`) {
		t.Fatalf("stderr = %q, want writer failure", got)
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
		assertContains(t, err.Error(), message)
	}
	assertContains(
		t,
		err.Error(),
		`skip item from source "feed-a": url "https://example.com/secret" must not contain userinfo`,
	)
	assertNotContains(t, err.Error(), credentialUsername, credentialPassword)
	assertCount(t, err.Error(), "--root=/tmp/evil", 1)
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
			assertCount(t, err.Error(), tt.ctxErr.Error(), 1)
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
	assertCount(t, stderr.String(), context.Canceled.Error(), 1)
	assertCount(t, stderr.String(), fetchErr.Error(), 1)
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
			assertCount(t, stderr.String(), "context canceled", 1)
			assertCount(t, stderr.String(), "mdhq process killed", 1)
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
