package thresh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Songmu/rssnip"
)

// FeedItem is the part of an rssnip item needed by the pipeline.
type FeedItem struct {
	URL string
}

// FeedFetcher fetches feed items for a half-open time window.
type FeedFetcher interface {
	Fetch(ctx context.Context, sourceURL string, since, until time.Time) ([]FeedItem, error)
}

// RSSnipFetcher fetches feeds with the rssnip library.
type RSSnipFetcher struct{}

// Fetch implements FeedFetcher.
func (RSSnipFetcher) Fetch(
	ctx context.Context,
	sourceURL string,
	since, until time.Time,
) ([]FeedItem, error) {
	normalizedSourceURL, err := normalizeSourceURLScheme(sourceURL)
	if err != nil {
		return nil, err
	}
	items, err := rssnip.Fetch(
		ctx,
		normalizedSourceURL,
		rssnip.WithSince(since),
		rssnip.WithUntil(until),
	)
	if err != nil {
		return nil, err
	}
	feedItems := make([]FeedItem, len(items))
	for i, item := range items {
		feedItems[i] = FeedItem{URL: item.URL}
	}
	return feedItems, nil
}

func normalizeSourceURLScheme(sourceURL string) (string, error) {
	if err := ValidateArticleURL(sourceURL); err != nil {
		return "", err
	}
	schemeEnd := strings.IndexByte(sourceURL, ':')
	return strings.ToLower(sourceURL[:schemeEnd]) + sourceURL[schemeEnd:], nil
}

// CollectedURL retains the feed that produced an article URL.
type CollectedURL struct {
	URL       string
	SourceURL string
}

// CollectFeeds collects unique article URLs in source and feed item order.
//
// Collection continues after source failures. The returned error joins every
// source failure and every rejected article URL, while the returned URLs
// contain all successfully collected items. Article URLs that are not absolute
// HTTP(S) URLs are rejected rather than forwarded to mdhq, because feed items
// are untrusted input that becomes a command-line argument.
func CollectFeeds(
	ctx context.Context,
	fetcher FeedFetcher,
	sources []string,
	since, until time.Time,
) ([]CollectedURL, error) {
	if fetcher == nil {
		return nil, errors.New("feed fetcher is required")
	}

	var (
		collected []CollectedURL
		failures  []error
	)
	seen := make(map[string]struct{})
	for _, sourceURL := range sources {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		items, err := fetcher.Fetch(ctx, sourceURL, since, until)
		if err != nil {
			ctxErr := ctx.Err()
			failure := fmt.Errorf("fetch source %q: %w", sourceURL, err)
			if ctxErr != nil && !errors.Is(failure, ctxErr) {
				failure = fmt.Errorf("%w (%w)", failure, ctxErr)
			}
			failures = append(failures, failure)
			if ctxErr != nil {
				break
			}
			continue
		}
		for _, item := range items {
			if item.URL == "" {
				continue
			}
			if _, ok := seen[item.URL]; ok {
				continue
			}
			seen[item.URL] = struct{}{}
			if err := ValidateArticleURL(item.URL); err != nil {
				failures = append(
					failures,
					fmt.Errorf("skip item from source %q: %w", sourceURL, err),
				)
				continue
			}
			collected = append(collected, CollectedURL{
				URL:       item.URL,
				SourceURL: sourceURL,
			})
		}
	}
	return collected, errors.Join(failures...)
}

// MDHQGetter saves one URL with mdhq.
type MDHQGetter interface {
	Get(ctx context.Context, url string, options MDHQOptions) (MDHQResult, error)
}

// PipelineRequest describes one feed collection and mdhq run.
type PipelineRequest struct {
	Sources []string
	Since   time.Time
	Until   time.Time
	Root    string
	Assets  bool
	Update  bool
}

// Pipeline coordinates feed collection and mdhq.
type Pipeline struct {
	Fetcher FeedFetcher
	MDHQ    MDHQGetter
}

// NewPipeline constructs a pipeline with injectable dependencies.
func NewPipeline(fetcher FeedFetcher, mdhq MDHQGetter) *Pipeline {
	return &Pipeline{Fetcher: fetcher, MDHQ: mdhq}
}

// Run executes the complete pipeline and writes compact successful results as
// JSON Lines to stdout. Diagnostics are written separately to stderr.
func (p *Pipeline) Run(
	ctx context.Context,
	request PipelineRequest,
	stdout, stderr io.Writer,
) error {
	if p == nil || p.Fetcher == nil {
		return errors.New("feed fetcher is required")
	}
	if p.MDHQ == nil {
		return errors.New("mdhq getter is required")
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}

	urls, collectErr := CollectFeeds(
		ctx,
		p.Fetcher,
		request.Sources,
		request.Since,
		request.Until,
	)
	var failures []error
	if collectErr != nil {
		failures = append(failures, collectErr)
		fmt.Fprintln(stderr, collectErr)
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	options := MDHQOptions{
		Root:   request.Root,
		Assets: request.Assets,
		Update: request.Update,
	}
	for _, article := range urls {
		if err := ctx.Err(); err != nil {
			if !errors.Is(collectErr, err) {
				failures = append(failures, err)
				fmt.Fprintln(stderr, err)
			}
			break
		}
		result, err := p.MDHQ.Get(ctx, article.URL, options)
		if err != nil {
			ctxErr := ctx.Err()
			failure := fmt.Errorf(
				"process URL %q from source %q: %w",
				article.URL,
				article.SourceURL,
				err,
			)
			if ctxErr != nil && !errors.Is(failure, ctxErr) {
				failure = fmt.Errorf("%w (%w)", failure, ctxErr)
			}
			failures = append(failures, failure)
			fmt.Fprintln(stderr, failure)
			if ctxErr != nil {
				break
			}
			continue
		}
		if result.Diagnostic != "" {
			fmt.Fprintf(stderr, "mdhq %q: %s\n", article.URL, result.Diagnostic)
		}
		if err := encoder.Encode(result); err != nil {
			failure := fmt.Errorf(
				"write result for URL %q from source %q: %w",
				article.URL,
				article.SourceURL,
				err,
			)
			failures = append(failures, failure)
			fmt.Fprintln(stderr, failure)
		}
	}
	return errors.Join(failures...)
}
