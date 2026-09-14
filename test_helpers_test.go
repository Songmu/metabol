package metabol

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type cliRunResult struct {
	stdout string
	stderr string
	err    error
}

func runCLIForTest(
	t *testing.T,
	ctx context.Context,
	args []string,
	now func() time.Time,
	lookupEnv LookupEnvFunc,
	pipeline pipelineRunner,
) cliRunResult {
	t.Helper()
	if ctx == nil {
		ctx = context.Background()
	}
	if now == nil {
		now = func() time.Time { return time.Time{} }
	}
	if lookupEnv == nil {
		lookupEnv = emptyLookup
	}
	var stdout, stderr bytes.Buffer
	err := run(ctx, args, &stdout, &stderr, now, lookupEnv, pipeline)
	return cliRunResult{
		stdout: stdout.String(),
		stderr: stderr.String(),
		err:    err,
	}
}

func writeTestConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(contents)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFixture(t *testing.T, path ...string) []byte {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(append([]string{"testdata"}, path...)...))
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

func emptyLookup(string) (string, bool) {
	return "", false
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func assertContains(t *testing.T, value, want string) {
	t.Helper()
	if !strings.Contains(value, want) {
		t.Errorf("%q does not contain %q", value, want)
	}
}

func assertNotContains(t *testing.T, value string, unwanted ...string) {
	t.Helper()
	for _, item := range unwanted {
		if strings.Contains(value, item) {
			t.Errorf("%q contains %q", value, item)
		}
	}
}

func assertCount(t *testing.T, value, substring string, want int) {
	t.Helper()
	if got := strings.Count(value, substring); got != want {
		t.Errorf("%q contains %q %d times, want %d", value, substring, got, want)
	}
}
