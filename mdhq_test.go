package thresh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
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
				"get", "--json", "--root", "/root",
				"--assets", "https://example.com/2",
			},
		},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("runner calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestMDHQGetAlwaysPassesResolvedAssetsFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		assets     bool
		wantFlag   string
		unwantFlag string
	}{
		{
			name:       "enabled",
			assets:     true,
			wantFlag:   "--assets",
			unwantFlag: "--no-assets",
		},
		{
			name:       "disabled",
			assets:     false,
			wantFlag:   "--no-assets",
			unwantFlag: "--assets",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runner := &fakeCommandRunner{
				responses: []fakeCommandResponse{{
					stdout: []byte(`{"requestedUrl":"https://example.com/article","sourceUrl":"https://example.com/article","path":"/root/article.md","status":"saved"}` + "\n"),
				}},
			}
			_, err := NewMDHQ(runner).Get(
				context.Background(),
				"https://example.com/article",
				MDHQOptions{Root: "/root", Assets: tt.assets},
			)
			if err != nil {
				t.Fatalf("MDHQ.Get error = %v", err)
			}

			var wantCount, unwantedCount int
			for _, arg := range runner.calls[0].args {
				switch arg {
				case tt.wantFlag:
					wantCount++
				case tt.unwantFlag:
					unwantedCount++
				}
			}
			if wantCount != 1 || unwantedCount != 0 {
				t.Fatalf(
					"asset flags in %q = %d %s and %d %s, want exactly one %s",
					runner.calls[0].args,
					wantCount,
					tt.wantFlag,
					unwantedCount,
					tt.unwantFlag,
					tt.wantFlag,
				)
			}
		})
	}
}

func TestMDHQGetAssetsFlagOverridesDisabledMDHQConfig(t *testing.T) {
	mdhqPath, err := exec.LookPath("mdhq")
	if err != nil {
		t.Skip("mdhq is not installed")
	}
	version, err := exec.Command(mdhqPath, "--version").Output()
	if err != nil {
		t.Fatalf("mdhq --version: %v", err)
	}
	if got := strings.TrimSpace(string(version)); got != "0.0.5" {
		t.Skipf("mdhq version = %q, want 0.0.5", got)
	}

	configHome := t.TempDir()
	configDir := filepath.Join(configHome, "mdhq")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("create mdhq config directory: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(configDir, "config.json"),
		[]byte("{\"assets\":false}\n"),
		0o600,
	); err != nil {
		t.Fatalf("write mdhq config: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	t.Setenv("no_proxy", "127.0.0.1,localhost")

	var imageRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/article":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html>
<html>
<head>
<title>Assets override fixture</title>
<meta property="og:image" content="%s/image.png">
</head>
<body>
<article>
<h1>Assets override fixture</h1>
<p>This local page verifies that the explicit positive assets flag overrides disabled mdhq configuration.</p>
<img src="%s/image.png" alt="fixture">
</article>
</body>
</html>`, serverURL(r), serverURL(r))
		case "/image.png":
			imageRequests.Add(1)
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("local image fixture"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	root := t.TempDir()
	result, err := (&MDHQ{
		Runner:  ExecCommandRunner{},
		Command: mdhqPath,
	}).Get(
		context.Background(),
		server.URL+"/article",
		MDHQOptions{Root: root, Assets: true},
	)
	if err != nil {
		t.Fatalf("MDHQ.Get error = %v", err)
	}
	if imageRequests.Load() == 0 {
		t.Fatal("image requests = 0, want at least one")
	}
	assets, err := filepath.Glob(filepath.Join(root, "_assets", "*.png"))
	if err != nil {
		t.Fatalf("glob downloaded assets: %v", err)
	}
	if len(assets) == 0 {
		t.Fatalf("downloaded assets = %v, want a PNG asset", assets)
	}
	if result.Status != "saved" {
		t.Fatalf("MDHQ.Get status = %q, want saved", result.Status)
	}
}

func serverURL(r *http.Request) string {
	return "http://" + r.Host
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
