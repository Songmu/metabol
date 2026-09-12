package thresh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// MDHQOptions controls mdhq storage behavior.
type MDHQOptions struct {
	Root   string
	Assets bool
	Update bool
}

// MDHQResult is the stable subset of mdhq's JSON result emitted by thresh.
type MDHQResult struct {
	RequestedURL string `json:"requestedUrl"`
	SourceURL    string `json:"sourceUrl"`
	Path         string `json:"path"`
	Status       string `json:"status"`
	Diagnostic   string `json:"-"`
}

// CommandRunner runs an external command and keeps its stdout and stderr
// separate.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
}

// ExecCommandRunner runs commands with os/exec.
type ExecCommandRunner struct{}

// Run implements CommandRunner.
func (ExecCommandRunner) Run(
	ctx context.Context,
	name string,
	args ...string,
) ([]byte, []byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// MDHQ invokes the mdhq command once per URL.
type MDHQ struct {
	Runner  CommandRunner
	Command string
}

// NewMDHQ constructs an mdhq client. A nil runner uses os/exec.
func NewMDHQ(runner CommandRunner) *MDHQ {
	if runner == nil {
		runner = ExecCommandRunner{}
	}
	return &MDHQ{Runner: runner, Command: "mdhq"}
}

// Get fetches one URL and returns its attributed mdhq result.
func (m *MDHQ) Get(
	ctx context.Context,
	url string,
	options MDHQOptions,
) (MDHQResult, error) {
	if m == nil || m.Runner == nil {
		return MDHQResult{}, errors.New("mdhq command runner is required")
	}
	if err := ValidateArticleURL(url); err != nil {
		return MDHQResult{}, fmt.Errorf("run mdhq: %w", err)
	}
	command := m.Command
	if command == "" {
		command = "mdhq"
	}
	args := []string{"get", "--json", "--root", options.Root}
	if !options.Assets {
		args = append(args, "--no-assets")
	}
	if options.Update {
		args = append(args, "--update")
	}
	args = append(args, url)

	stdout, stderr, err := m.Runner.Run(ctx, command, args...)
	if err != nil {
		message := strings.TrimSpace(string(stderr))
		if message == "" {
			return MDHQResult{}, fmt.Errorf("run mdhq for %q: %w", url, err)
		}
		return MDHQResult{}, fmt.Errorf("run mdhq for %q: %w: %s", url, err, message)
	}

	var result MDHQResult
	decoder := json.NewDecoder(bytes.NewReader(stdout))
	if err := decoder.Decode(&result); err != nil {
		return MDHQResult{}, fmt.Errorf("decode mdhq result for %q: %w", url, err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return MDHQResult{}, fmt.Errorf("decode mdhq result for %q: multiple results", url)
		}
		return MDHQResult{}, fmt.Errorf("decode mdhq result for %q: %w", url, err)
	}
	if result.RequestedURL == "" {
		return MDHQResult{}, fmt.Errorf("decode mdhq result for %q: requestedUrl is empty", url)
	}
	if result.SourceURL == "" {
		return MDHQResult{}, fmt.Errorf("decode mdhq result for %q: sourceUrl is empty", url)
	}
	if result.Path == "" {
		return MDHQResult{}, fmt.Errorf("decode mdhq result for %q: path is empty", url)
	}
	switch result.Status {
	case "saved", "updated", "unchanged", "skipped":
		result.Diagnostic = strings.TrimSpace(string(stderr))
		return result, nil
	default:
		return MDHQResult{}, fmt.Errorf(
			"decode mdhq result for %q: unknown status %q",
			url,
			result.Status,
		)
	}
}
