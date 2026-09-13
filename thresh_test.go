package thresh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type recordingPipeline struct {
	request  PipelineRequest
	requests []PipelineRequest
}

func (p *recordingPipeline) Run(
	_ context.Context,
	request PipelineRequest,
	_, _ io.Writer,
) error {
	p.request = request
	p.requests = append(p.requests, request)
	return nil
}

type integrationMDHQ struct {
	urls []string
}

func (m *integrationMDHQ) Get(
	_ context.Context,
	url string,
	_ MDHQOptions,
) (MDHQResult, error) {
	m.urls = append(m.urls, url)
	return MDHQResult{
		RequestedURL: url,
		SourceURL:    url,
		Path:         "/articles/item.md",
		Status:       "saved",
	}, nil
}

func TestRunVersionDoesNotRequireConfig(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(
		context.Background(),
		[]string{"--version"},
		&stdout,
		&stderr,
		func() time.Time { return time.Time{} },
		func(string) (string, bool) { return "", false },
		&recordingPipeline{},
	)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if got, want := stdout.String(), "thresh v0.0.0 (rev:HEAD)\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunResolvesConfigAndSelectsLastCompleteWindow(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(configPath, []byte(`
root: ./articles
assets: true
update: false
timezone: Asia/Tokyo
window:
  daily: "07:00"
sources:
  - https://example.com/feed.xml
  - url: https://example.org/feed.xml
    name: example
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	pipeline := &recordingPipeline{}
	var stdout, stderr bytes.Buffer
	err = run(
		context.Background(),
		[]string{"--config", configPath, "--update"},
		&stdout,
		&stderr,
		func() time.Time {
			return time.Date(2026, 9, 12, 19, 0, 0, 0, time.FixedZone("JST", 9*60*60))
		},
		func(string) (string, bool) { return "", false },
		pipeline,
	)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	if got, want := pipeline.request.Sources, []string{
		"https://example.com/feed.xml",
		"https://example.org/feed.xml",
	}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("sources = %v, want %v", got, want)
	}
	jst, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pipeline.request.Since, time.Date(2026, 9, 11, 7, 0, 0, 0, jst); !got.Equal(want) {
		t.Fatalf("since = %s, want %s", got, want)
	}
	if got, want := pipeline.request.Until, time.Date(2026, 9, 12, 7, 0, 0, 0, jst); !got.Equal(want) {
		t.Fatalf("until = %s, want %s", got, want)
	}
	if !pipeline.request.Assets || !pipeline.request.Update {
		t.Fatalf("assets/update = %v/%v, want true/true",
			pipeline.request.Assets, pipeline.request.Update)
	}
	if got, want := pipeline.request.Root, filepath.Join(filepath.Dir(configPath), "articles"); got != want {
		t.Fatalf("root = %q, want %q", got, want)
	}
}

func TestRunAtSelectsContainingWindow(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(configPath, []byte(`
root: ./articles
timezone: Asia/Tokyo
window:
  daily: "07:00"
sources:
  - https://example.com/feed.xml
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	pipeline := &recordingPipeline{}
	err = run(
		context.Background(),
		[]string{"--config", configPath, "--at", "2026-09-11T07:00:00+09:00"},
		io.Discard,
		io.Discard,
		func() time.Time { return time.Time{} },
		func(string) (string, bool) { return "", false },
		pipeline,
	)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	jst, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pipeline.request.Since, time.Date(2026, 9, 11, 7, 0, 0, 0, jst); !got.Equal(want) {
		t.Fatalf("since = %s, want %s", got, want)
	}
	if got, want := pipeline.request.Until, time.Date(2026, 9, 12, 7, 0, 0, 0, jst); !got.Equal(want) {
		t.Fatalf("until = %s, want %s", got, want)
	}
}

func TestRunProcessesMultipleWindowsOldestFirst(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(configPath, []byte(`
root: ./articles
timezone: UTC
window:
  daily: "07:00"
  count: 2
sources:
  - https://example.com/feed.xml
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	pipeline := &recordingPipeline{}
	err = run(
		context.Background(),
		[]string{"--config", configPath, "--at", "2026-09-11T19:00:00Z", "--window-count", "3"},
		io.Discard,
		io.Discard,
		func() time.Time { return time.Time{} },
		func(string) (string, bool) { return "", false },
		pipeline,
	)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if got, want := len(pipeline.requests), 3; got != want {
		t.Fatalf("pipeline runs = %d, want %d", got, want)
	}
	assertPipelineWindow(t, pipeline.requests[0], "2026-09-09T07:00:00Z", "2026-09-10T07:00:00Z")
	assertPipelineWindow(t, pipeline.requests[1], "2026-09-10T07:00:00Z", "2026-09-11T07:00:00Z")
	assertPipelineWindow(t, pipeline.requests[2], "2026-09-11T07:00:00Z", "2026-09-12T07:00:00Z")
}

func assertPipelineWindow(t *testing.T, request PipelineRequest, wantSince, wantUntil string) {
	t.Helper()
	since, err := time.Parse(time.RFC3339, wantSince)
	if err != nil {
		t.Fatal(err)
	}
	until, err := time.Parse(time.RFC3339, wantUntil)
	if err != nil {
		t.Fatal(err)
	}
	if !request.Since.Equal(since) || !request.Until.Equal(until) {
		t.Fatalf(
			"pipeline window = [%s, %s), want [%s, %s)",
			request.Since,
			request.Until,
			since,
			until,
		)
	}
}

type failingPipeline struct {
	err error
}

func (p *failingPipeline) Run(
	_ context.Context,
	_ PipelineRequest,
	_, errStream io.Writer,
) error {
	fmt.Fprintln(errStream, p.err)
	return p.err
}

type failFirstPipeline struct {
	err      error
	requests []PipelineRequest
}

func (p *failFirstPipeline) Run(
	_ context.Context,
	request PipelineRequest,
	_, errStream io.Writer,
) error {
	p.requests = append(p.requests, request)
	if len(p.requests) == 1 {
		fmt.Fprintln(errStream, p.err)
		return p.err
	}
	return nil
}

type cancelingPipeline struct {
	cancel   context.CancelFunc
	requests []PipelineRequest
}

func (p *cancelingPipeline) Run(
	_ context.Context,
	request PipelineRequest,
	_, _ io.Writer,
) error {
	p.requests = append(p.requests, request)
	p.cancel()
	return nil
}

func TestRunChecksCancellationBeforeEachWindow(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(configPath, []byte(`
root: ./articles
timezone: UTC
window:
  daily: "07:00"
  count: 2
sources:
  - https://example.com/feed.xml
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	pipeline := &cancelingPipeline{cancel: cancel}
	var stderr bytes.Buffer
	err = run(
		ctx,
		[]string{"--config", configPath},
		io.Discard,
		&stderr,
		func() time.Time { return time.Date(2026, 9, 12, 19, 0, 0, 0, time.UTC) },
		func(string) (string, bool) { return "", false },
		pipeline,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run error = %v, want context.Canceled", err)
	}
	if got, want := len(pipeline.requests), 1; got != want {
		t.Fatalf("pipeline runs = %d, want %d", got, want)
	}
	if got, want := stderr.String(), context.Canceled.Error()+"\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestRunContinuesAfterWindowFailure(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(configPath, []byte(`
root: ./articles
timezone: UTC
window:
  daily: "07:00"
  count: 2
sources:
  - https://example.com/feed.xml
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	failure := errors.New("first window failed")
	pipeline := &failFirstPipeline{err: failure}
	err = run(
		context.Background(),
		[]string{"--config", configPath},
		io.Discard,
		io.Discard,
		func() time.Time { return time.Date(2026, 9, 12, 19, 0, 0, 0, time.UTC) },
		func(string) (string, bool) { return "", false },
		pipeline,
	)
	if !errors.Is(err, failure) {
		t.Fatalf("run error = %v, want wrapping %v", err, failure)
	}
	if got, want := len(pipeline.requests), 2; got != want {
		t.Fatalf("pipeline runs = %d, want %d", got, want)
	}
}

func TestRunDoesNotRepeatAlreadyReportedFailures(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(configPath, []byte(`
root: ./articles
timezone: UTC
window:
  daily: "07:00"
sources:
  - https://example.com/feed.xml
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	reported := errors.New(`fetch source "https://example.com/feed.xml": boom`)
	var stdout, stderr bytes.Buffer
	err = run(
		context.Background(),
		[]string{"--config", configPath},
		&stdout,
		&stderr,
		func() time.Time { return time.Date(2026, 9, 12, 19, 0, 0, 0, time.UTC) },
		func(string) (string, bool) { return "", false },
		&failingPipeline{err: reported},
	)
	if err == nil {
		t.Fatal("run error = nil, want a non-nil error for a non-zero exit")
	}
	if !errors.Is(err, reported) {
		t.Fatalf("run error %v does not wrap the pipeline failure", err)
	}
	if strings.Contains(err.Error(), "boom") {
		t.Fatalf("run error %q repeats details already written to stderr", err)
	}
	if got := strings.Count(stderr.String(), "boom"); got != 1 {
		t.Fatalf("stderr reported the failure %d times, want 1: %q", got, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunEndToEndWithRSSnip(t *testing.T) {
	feed := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Example</title>
    <link>https://example.com/</link>
    <description>Example feed</description>
    <item>
      <guid>included</guid>
      <link>https://example.com/included</link>
      <pubDate>Fri, 11 Sep 2026 00:00:00 GMT</pubDate>
    </item>
    <item>
      <guid>excluded</guid>
      <link>https://example.com/excluded</link>
      <pubDate>Thu, 10 Sep 2026 00:00:00 GMT</pubDate>
    </item>
  </channel>
</rss>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, feed)
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(configPath, []byte(fmt.Sprintf(`
root: %s
timezone: Asia/Tokyo
window:
  daily: "07:00"
sources:
  - %s
`, filepath.Join(t.TempDir(), "articles"), server.URL)), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	mdhq := &integrationMDHQ{}
	pipeline := NewPipeline(RSSnipFetcher{}, mdhq)
	var stdout, stderr bytes.Buffer
	err = run(
		context.Background(),
		[]string{
			"--config", configPath,
			"--at", "2026-09-11T08:00:00+09:00",
		},
		&stdout,
		&stderr,
		func() time.Time { return time.Time{} },
		func(string) (string, bool) { return "", false },
		pipeline,
	)
	if err != nil {
		t.Fatalf("run returned error: %v\nstderr: %s", err, stderr.String())
	}
	if got, want := mdhq.urls, []string{"https://example.com/included"}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("processed URLs = %v, want %v", got, want)
	}
	const want = `{"requestedUrl":"https://example.com/included","sourceUrl":"https://example.com/included","path":"/articles/item.md","status":"saved"}` + "\n"
	if got := stdout.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}
