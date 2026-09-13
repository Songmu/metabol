package metabol

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInitCreatesValidSampleInEmptyDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer

	err := runInitAt(nil, &stdout, &stderr, dir, func(string, bool) bool {
		t.Fatal("confirm called for empty directory")
		return false
	})
	if err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(dir, DefaultConfigPath)
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, sampleConfig) {
		t.Errorf("generated config differs from embedded sample:\n%s", got)
	}
	if _, err := LoadConfig(configPath); err != nil {
		t.Fatalf("generated config is invalid: %v", err)
	}
	for _, want := range []string{
		"Created metabol.yaml.",
		"Edit metabol.yaml",
		"mdhq",
		"Run metabol.",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunInitConfirmsInNonEmptyDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("articles/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	var message string
	var defaultToYes bool

	err := runInitAt(nil, &stdout, &stderr, dir, func(gotMessage string, gotDefault bool) bool {
		message = gotMessage
		defaultToYes = gotDefault
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(message, "not empty") {
		t.Errorf("prompt = %q, want non-empty directory warning", message)
	}
	if defaultToYes {
		t.Error("confirmation defaults to Yes, want No")
	}
	if _, err := os.Stat(filepath.Join(dir, DefaultConfigPath)); err != nil {
		t.Fatalf("generated config: %v", err)
	}
}

func TestRunInitCancellationLeavesDirectoryUnchanged(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	existingPath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(existingPath, []byte("existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	err := runInitAt(nil, &stdout, &stderr, dir, func(string, bool) bool {
		return false
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stdout.String(), "Initialization canceled.\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, DefaultConfigPath)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("metabol.yaml exists or returned unexpected error: %v", err)
	}
	got, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "existing\n" {
		t.Errorf("existing file changed: %q", got)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunInitDoesNotOverwriteExistingConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, DefaultConfigPath)
	const existing = "existing: true\n"
	if err := os.WriteFile(configPath, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	err := runInitAt(nil, &stdout, &stderr, dir, func(string, bool) bool {
		t.Fatal("confirm called when metabol.yaml already exists")
		return false
	})
	if err == nil || !strings.Contains(err.Error(), "metabol.yaml already exists") {
		t.Fatalf("error = %v, want existing-file error", err)
	}
	got, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != existing {
		t.Errorf("existing config changed: %q", got)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q", stdout.String())
	}
}

func TestRunInitUsageAndArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		wantErr    error
		wantError  string
		wantStderr string
	}{
		{
			name:       "help",
			args:       []string{"-h"},
			wantErr:    flag.ErrHelp,
			wantStderr: "Usage: metabol init",
		},
		{
			name:      "unexpected argument",
			args:      []string{"extra"},
			wantError: "unexpected arguments",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := runInitAt(tt.args, &stdout, &stderr, t.TempDir(), nil)
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantError != "" && (err == nil || !strings.Contains(err.Error(), tt.wantError)) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantError)
			}
			if tt.wantStderr != "" {
				assertContains(t, stderr.String(), tt.wantStderr)
			}
		})
	}
}

func TestRunDispatchesInitBeforeConfigLoading(t *testing.T) {
	t.Parallel()
	result := runCLIForTest(
		t,
		t.Context(),
		[]string{"init", "extra"},
		nil,
		nil,
		&recordingPipeline{},
	)
	if result.err == nil || !strings.Contains(result.err.Error(), "unexpected arguments") {
		t.Fatalf("error = %v, want init argument error", result.err)
	}
}

func TestHelpMentionsInitSubcommand(t *testing.T) {
	t.Parallel()
	result := runCLIForTest(
		t,
		t.Context(),
		[]string{"-h"},
		nil,
		nil,
		&recordingPipeline{},
	)
	if !errors.Is(result.err, flag.ErrHelp) {
		t.Fatalf("error = %v, want flag.ErrHelp", result.err)
	}
	if want := "init    Create a sample metabol.yaml"; !strings.Contains(result.stderr, want) {
		t.Errorf("stderr missing %q:\n%s", want, result.stderr)
	}
}
