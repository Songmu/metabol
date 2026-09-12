package thresh

import (
	"bytes"
	"context"
	"errors"
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
}

func (m *fakeMDHQ) Get(
	_ context.Context,
	url string,
	options MDHQOptions,
) (MDHQResult, error) {
	m.calls = append(m.calls, mdhqCall{url: url, options: options})
	response := m.responses[url]
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

	got, err := CollectFeeds(
		context.Background(),
		fetcher,
		[]string{"feed-a"},
		time.Time{},
		time.Time{},
	)
	want := []CollectedURL{{URL: "https://example.com/ok", SourceURL: "feed-a"}}
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
		if !strings.Contains(err.Error(), message) {
			t.Errorf("CollectFeeds error %q does not contain %q", err, message)
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
