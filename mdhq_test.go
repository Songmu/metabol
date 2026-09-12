package thresh

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type commandCall struct {
	name string
	args []string
}

type fakeCommandResponse struct {
	stdout []byte
	stderr []byte
	err    error
}

type fakeCommandRunner struct {
	responses []fakeCommandResponse
	calls     []commandCall
}

func (r *fakeCommandRunner) Run(
	_ context.Context,
	name string,
	args ...string,
) ([]byte, []byte, error) {
	r.calls = append(r.calls, commandCall{
		name: name,
		args: append([]string(nil), args...),
	})
	response := r.responses[len(r.calls)-1]
	return response.stdout, response.stderr, response.err
}

func TestMDHQGetBuildsFlagsAndDecodesResult(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{
		responses: []fakeCommandResponse{
			{
				stdout: []byte(`{"requestedUrl":"https://example.com/1","sourceUrl":"https://example.com/final","path":"/root/1.md","status":"updated","assets":[],"warnings":[]}` + "\n"),
			},
			{
				stdout: []byte(`{"requestedUrl":"https://example.com/2","sourceUrl":"https://example.com/2","path":"/root/2.md","status":"skipped"}` + "\n"),
			},
		},
	}
	mdhq := NewMDHQ(runner)

	got, err := mdhq.Get(context.Background(), "https://example.com/1", MDHQOptions{
		Root:   "/root",
		Assets: false,
		Update: true,
	})
	if err != nil {
		t.Fatalf("MDHQ.Get error = %v", err)
	}
	want := MDHQResult{
		RequestedURL: "https://example.com/1",
		SourceURL:    "https://example.com/final",
		Path:         "/root/1.md",
		Status:       "updated",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MDHQ.Get result = %#v, want %#v", got, want)
	}

	if _, err := mdhq.Get(
		context.Background(),
		"https://example.com/2",
		MDHQOptions{Root: "/root", Assets: true},
	); err != nil {
		t.Fatalf("MDHQ.Get second error = %v", err)
	}
	wantCalls := []commandCall{
		{
			name: "mdhq",
			args: []string{
				"get", "--json", "--root", "/root",
				"--no-assets", "--update", "https://example.com/1",
			},
		},
		{
			name: "mdhq",
			args: []string{
				"get", "--json", "--root", "/root", "https://example.com/2",
			},
		},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("runner calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestMDHQGetReportsCommandFailureWithSeparateStderr(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{
		responses: []fakeCommandResponse{{
			stdout: []byte("not a result"),
			stderr: []byte("network failed\n"),
			err:    errors.New("exit status 1"),
		}},
	}
	_, err := NewMDHQ(runner).Get(
		context.Background(),
		"https://example.com/failed",
		MDHQOptions{Root: "/root"},
	)
	if err == nil {
		t.Fatal("MDHQ.Get error = nil")
	}
	if got := err.Error(); !strings.Contains(got, "exit status 1: network failed") {
		t.Fatalf("MDHQ.Get error = %q", got)
	}
}

func TestMDHQGetRejectsUnknownStatus(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{
		responses: []fakeCommandResponse{{
			stdout: []byte(`{"requestedUrl":"https://example.com/1","sourceUrl":"https://example.com/1","path":"/root/1.md","status":"mystery"}` + "\n"),
		}},
	}
	_, err := NewMDHQ(runner).Get(
		context.Background(),
		"https://example.com/1",
		MDHQOptions{Root: "/root"},
	)
	if err == nil || !strings.Contains(err.Error(), `unknown status "mystery"`) {
		t.Fatalf("MDHQ.Get error = %v", err)
	}
}

func TestMDHQGetAcceptsNormalizedRequestedURL(t *testing.T) {
	t.Parallel()

	rawURL := "HTTPS://EXAMPLE.com:443/article#fragment"
	runner := &fakeCommandRunner{
		responses: []fakeCommandResponse{{
			stdout: []byte(`{"requestedUrl":"https://example.com/article","sourceUrl":"https://example.com/article","path":"/root/article.md","status":"saved"}` + "\n"),
			stderr: []byte("warning from mdhq\n"),
		}},
	}
	got, err := NewMDHQ(runner).Get(
		context.Background(),
		rawURL,
		MDHQOptions{Root: "/root"},
	)
	if err != nil {
		t.Fatalf("MDHQ.Get error = %v", err)
	}
	if got.RequestedURL != "https://example.com/article" {
		t.Fatalf("requestedUrl = %q", got.RequestedURL)
	}
	if got.Diagnostic != "warning from mdhq" {
		t.Fatalf("diagnostic = %q", got.Diagnostic)
	}
	if got := runner.calls[0].args[len(runner.calls[0].args)-1]; got != rawURL {
		t.Fatalf("mdhq URL argument = %q, want %q", got, rawURL)
	}
}

func TestMDHQGetRejectsInvalidOrMultipleResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		stdout string
		want   string
	}{
		{
			name:   "empty requested URL",
			stdout: `{"requestedUrl":"","sourceUrl":"https://example.com/other","path":"/root/1.md","status":"saved"}`,
			want:   "requestedUrl is empty",
		},
		{
			name: "multiple results",
			stdout: "" +
				`{"requestedUrl":"https://example.com/1","sourceUrl":"https://example.com/1","path":"/root/1.md","status":"saved"}` + "\n" +
				`{"requestedUrl":"https://example.com/1","sourceUrl":"https://example.com/1","path":"/root/1.md","status":"saved"}`,
			want: "multiple results",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := &fakeCommandRunner{
				responses: []fakeCommandResponse{{stdout: []byte(tt.stdout)}},
			}
			_, err := NewMDHQ(runner).Get(
				context.Background(),
				"https://example.com/1",
				MDHQOptions{Root: "/root"},
			)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MDHQ.Get error = %v, want substring %q", err, tt.want)
			}
		})
	}
}
