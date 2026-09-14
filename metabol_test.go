package metabol

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
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
	result := runCLIForTest(
		t,
		context.Background(),
		[]string{"--version"},
		nil,
		nil,
		&recordingPipeline{},
	)
	if result.err != nil {
		t.Fatalf("run returned error: %v", result.err)
	}
	if got, want := result.stdout,
		fmt.Sprintf("metabol v%s (rev:HEAD)\n", version); got != want {

		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if result.stderr != "" {
		t.Fatalf("stderr = %q, want empty", result.stderr)
	}
}

func TestRunResolvesConfigAndSelectsLastCompleteWindow(t *testing.T) {
	configPath := writeTestConfig(t, `
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
`)

	pipeline := &recordingPipeline{}
	result := runCLIForTest(
		t,
		context.Background(),
		[]string{"--config", configPath, "--update"},
		func() time.Time {
			return time.Date(2026, 9, 12, 19, 0, 0, 0, time.FixedZone("JST", 9*60*60))
		},
		nil,
		pipeline,
	)
	if result.err != nil {
		t.Fatalf("run returned error: %v", result.err)
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
	configPath := writeTestConfig(t, `
root: ./articles
timezone: Asia/Tokyo
window:
  daily: "07:00"
sources:
  - https://example.com/feed.xml
`)

	pipeline := &recordingPipeline{}
	result := runCLIForTest(
		t,
		context.Background(),
		[]string{"--config", configPath, "--at", "2026-09-11T07:00:00+09:00"},
		nil,
		nil,
		pipeline,
	)
	if result.err != nil {
		t.Fatalf("run returned error: %v", result.err)
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
	configPath := writeTestConfig(t, `
root: ./articles
timezone: UTC
window:
  daily: "07:00"
  count: 2
sources:
  - https://example.com/feed.xml
`)

	pipeline := &recordingPipeline{}
	result := runCLIForTest(
		t,
		context.Background(),
		[]string{"--config", configPath, "--at", "2026-09-11T19:00:00Z", "--window-count", "3"},
		nil,
		nil,
		pipeline,
	)
	if result.err != nil {
		t.Fatalf("run returned error: %v", result.err)
	}
	if got, want := len(pipeline.requests), 3; got != want {
		t.Fatalf("pipeline runs = %d, want %d", got, want)
	}
	assertPipelineWindow(t, pipeline.requests[0], "2026-09-09T07:00:00Z", "2026-09-10T07:00:00Z")
	assertPipelineWindow(t, pipeline.requests[1], "2026-09-10T07:00:00Z", "2026-09-11T07:00:00Z")
	assertPipelineWindow(t, pipeline.requests[2], "2026-09-11T07:00:00Z", "2026-09-12T07:00:00Z")
}

func TestRunCatchupOmitsUntilForNewestWindow(t *testing.T) {
	configPath := writeTestConfig(t, `
root: ./articles
timezone: UTC
window:
  daily: "07:00"
  count: 3
sources:
  - https://example.com/feed.xml
`)

	pipeline := &recordingPipeline{}
	result := runCLIForTest(
		t,
		context.Background(),
		[]string{"--config", configPath, "--catchup"},
		func() time.Time { return time.Date(2026, 9, 12, 19, 0, 0, 0, time.UTC) },
		nil,
		pipeline,
	)
	if result.err != nil {
		t.Fatalf("run returned error: %v", result.err)
	}
	if got, want := len(pipeline.requests), 3; got != want {
		t.Fatalf("pipeline runs = %d, want %d", got, want)
	}
	assertPipelineWindow(t, pipeline.requests[0], "2026-09-09T07:00:00Z", "2026-09-10T07:00:00Z")
	assertPipelineWindow(t, pipeline.requests[1], "2026-09-10T07:00:00Z", "2026-09-11T07:00:00Z")
	if got, want := pipeline.requests[2].Since, time.Date(2026, 9, 11, 7, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("since = %s, want %s", got, want)
	}
	if pipeline.requests[2].Until != nil {
		t.Fatalf("until = %s, want nil", pipeline.requests[2].Until)
	}
}

func TestRunAtOverridesCatchupWithWarning(t *testing.T) {
	configPath := writeTestConfig(t, `
root: ./articles
catchup: true
timezone: UTC
window:
  daily: "07:00"
sources:
  - https://example.com/feed.xml
`)

	pipeline := &recordingPipeline{}
	result := runCLIForTest(
		t,
		context.Background(),
		[]string{"--config", configPath, "--at", "2026-09-11T19:00:00Z"},
		nil,
		nil,
		pipeline,
	)
	if result.err != nil {
		t.Fatalf("run returned error: %v", result.err)
	}
	assertPipelineWindow(t, pipeline.request, "2026-09-11T07:00:00Z", "2026-09-12T07:00:00Z")
	if got, want := result.stderr, "warning: at is set; catchup is ignored\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
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
	if request.Until == nil {
		t.Fatalf("pipeline window = [%s, nil), want [%s, %s)", request.Since, since, until)
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

type cancelingFailingPipeline struct {
	cancel context.CancelFunc
	err    error
}

func (p *cancelingFailingPipeline) Run(
	_ context.Context,
	_ PipelineRequest,
	_, errStream io.Writer,
) error {
	fmt.Fprintln(errStream, p.err)
	p.cancel()
	return p.err
}

func TestRunChecksCancellationBeforeEachWindow(t *testing.T) {
	configPath := writeTestConfig(t, `
root: ./articles
timezone: UTC
window:
  daily: "07:00"
  count: 2
sources:
  - https://example.com/feed.xml
`)

	ctx, cancel := context.WithCancel(context.Background())
	pipeline := &cancelingPipeline{cancel: cancel}
	result := runCLIForTest(
		t,
		ctx,
		[]string{"--config", configPath},
		func() time.Time { return time.Date(2026, 9, 12, 19, 0, 0, 0, time.UTC) },
		nil,
		pipeline,
	)
	if !errors.Is(result.err, context.Canceled) {
		t.Fatalf("run error = %v, want context.Canceled", result.err)
	}
	if got, want := len(pipeline.requests), 1; got != want {
		t.Fatalf("pipeline runs = %d, want %d", got, want)
	}
	if got, want := result.stderr, context.Canceled.Error()+"\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestRunChecksCancellationAfterFinalWindow(t *testing.T) {
	configPath := writeTestConfig(t, `
root: ./articles
timezone: UTC
window:
  daily: "07:00"
sources:
  - https://example.com/feed.xml
`)

	ctx, cancel := context.WithCancel(context.Background())
	pipeline := &cancelingPipeline{cancel: cancel}
	result := runCLIForTest(
		t,
		ctx,
		[]string{"--config", configPath},
		func() time.Time { return time.Date(2026, 9, 12, 19, 0, 0, 0, time.UTC) },
		nil,
		pipeline,
	)

	if !errors.Is(result.err, context.Canceled) {
		t.Fatalf("run error = %v, want context.Canceled", result.err)
	}
	if got, want := len(pipeline.requests), 1; got != want {
		t.Fatalf("pipeline runs = %d, want %d", got, want)
	}
	if got, want := result.stderr, context.Canceled.Error()+"\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestRunAddsCancellationToConcurrentPipelineFailure(t *testing.T) {
	configPath := writeTestConfig(t, `
root: ./articles
timezone: UTC
window:
  daily: "07:00"
sources:
  - https://example.com/feed.xml
`)

	ctx, cancel := context.WithCancel(context.Background())
	failure := errors.New("pipeline failed")
	result := runCLIForTest(
		t,
		ctx,
		[]string{"--config", configPath},
		func() time.Time { return time.Date(2026, 9, 12, 19, 0, 0, 0, time.UTC) },
		nil,
		&cancelingFailingPipeline{cancel: cancel, err: failure},
	)

	if !errors.Is(result.err, failure) {
		t.Fatalf("run error = %v, want wrapping %v", result.err, failure)
	}
	if !errors.Is(result.err, context.Canceled) {
		t.Fatalf("run error = %v, want context.Canceled", result.err)
	}
	assertCount(t, result.stderr, failure.Error(), 1)
	assertCount(t, result.stderr, context.Canceled.Error(), 1)
}

func TestRunContinuesAfterWindowFailure(t *testing.T) {
	configPath := writeTestConfig(t, `
root: ./articles
timezone: UTC
window:
  daily: "07:00"
  count: 2
sources:
  - https://example.com/feed.xml
`)
	failure := errors.New("first window failed")
	pipeline := &failFirstPipeline{err: failure}
	result := runCLIForTest(
		t,
		context.Background(),
		[]string{"--config", configPath},
		func() time.Time { return time.Date(2026, 9, 12, 19, 0, 0, 0, time.UTC) },
		nil,
		pipeline,
	)
	if !errors.Is(result.err, failure) {
		t.Fatalf("run error = %v, want wrapping %v", result.err, failure)
	}
	if got, want := len(pipeline.requests), 2; got != want {
		t.Fatalf("pipeline runs = %d, want %d", got, want)
	}
}

func TestRunDoesNotRepeatAlreadyReportedFailures(t *testing.T) {
	configPath := writeTestConfig(t, `
root: ./articles
timezone: UTC
window:
  daily: "07:00"
sources:
  - https://example.com/feed.xml
`)

	reported := errors.New(`fetch source "https://example.com/feed.xml": boom`)
	result := runCLIForTest(
		t,
		context.Background(),
		[]string{"--config", configPath},
		func() time.Time { return time.Date(2026, 9, 12, 19, 0, 0, 0, time.UTC) },
		nil,
		&failingPipeline{err: reported},
	)
	if result.err == nil {
		t.Fatal("run error = nil, want a non-nil error for a non-zero exit")
	}
	if !errors.Is(result.err, reported) {
		t.Fatalf("run error %v does not wrap the pipeline failure", result.err)
	}
	if strings.Contains(result.err.Error(), "boom") {
		t.Fatalf("run error %q repeats details already written to stderr", result.err)
	}
	assertCount(t, result.stderr, "boom", 1)
	if result.stdout != "" {
		t.Fatalf("stdout = %q, want empty", result.stdout)
	}
}

func TestRunEndToEndWithRSSnip(t *testing.T) {
	feed := readFixture(t, "rss", "window-filter.xml")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(feed)
	}))
	defer server.Close()

	configPath := writeTestConfig(t, fmt.Sprintf(`
root: %s
timezone: Asia/Tokyo
window:
  daily: "07:00"
sources:
  - %s
`, filepath.Join(t.TempDir(), "articles"), server.URL))

	mdhq := &integrationMDHQ{}
	pipeline := NewPipeline(RSSnipFetcher{}, mdhq)
	result := runCLIForTest(
		t,
		context.Background(),
		[]string{
			"--config", configPath,
			"--at", "2026-09-11T08:00:00+09:00",
		},
		nil,
		nil,
		pipeline,
	)
	if result.err != nil {
		t.Fatalf("run returned error: %v\nstderr: %s", result.err, result.stderr)
	}
	if got, want := mdhq.urls, []string{"https://example.com/included"}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("processed URLs = %v, want %v", got, want)
	}
	const want = `{"requestedUrl":"https://example.com/included","sourceUrl":"https://example.com/included","path":"/articles/item.md","status":"saved"}` + "\n"
	if got := result.stdout; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if result.stderr != "" {
		t.Fatalf("stderr = %q, want empty", result.stderr)
	}
}

func TestRunCatchupEndToEndWithRSSnip(t *testing.T) {
	feed := readFixture(t, "rss", "window-filter.xml")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(feed)
	}))
	defer server.Close()

	configPath := writeTestConfig(t, fmt.Sprintf(`
root: %s
timezone: Asia/Tokyo
window:
  daily: "07:00"
sources:
  - %s
`, filepath.Join(t.TempDir(), "articles"), server.URL))

	mdhq := &integrationMDHQ{}
	result := runCLIForTest(
		t,
		context.Background(),
		[]string{"--config", configPath, "--catchup"},
		func() time.Time {
			return time.Date(2026, 9, 12, 19, 0, 0, 0, time.FixedZone("JST", 9*60*60))
		},
		nil,
		NewPipeline(RSSnipFetcher{}, mdhq),
	)
	if result.err != nil {
		t.Fatalf("run returned error: %v\nstderr: %s", result.err, result.stderr)
	}
	want := []string{
		"https://example.com/ongoing",
		"https://example.com/included",
	}
	if !reflect.DeepEqual(mdhq.urls, want) {
		t.Fatalf("processed URLs = %v, want %v", mdhq.urls, want)
	}
}
