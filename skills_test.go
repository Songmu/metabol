package metabol

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSkillsList(t *testing.T) {
	t.Parallel()
	result := runCLIForTest(
		t,
		context.Background(),
		[]string{"skills", "list"},
		nil,
		nil,
		&recordingPipeline{},
	)
	if result.err != nil {
		t.Fatal(result.err)
	}
	if got := result.stdout; !strings.Contains(got, "metabol") ||
		!strings.Contains(got, "collect articles") {
		t.Errorf("stdout = %q", got)
	}
	if result.stderr != "" {
		t.Errorf("stderr = %q", result.stderr)
	}
}

func TestRunSkillsUsage(t *testing.T) {
	t.Parallel()
	result := runCLIForTest(
		t,
		context.Background(),
		[]string{"skills"},
		nil,
		nil,
		&recordingPipeline{},
	)
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.stdout != "" {
		t.Errorf("stdout = %q", result.stdout)
	}
	for _, want := range []string{
		"Usage: metabol skills <command> [options]",
		"install",
		"status",
		"uninstall",
	} {
		if !strings.Contains(result.stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, result.stderr)
		}
	}
}

func TestRunSkillsInstallDryRun(t *testing.T) {
	t.Parallel()
	prefix := filepath.Join(t.TempDir(), "skills")
	result := runCLIForTest(
		t,
		context.Background(),
		[]string{"skills", "install", "--dry-run", "--prefix", prefix},
		nil,
		nil,
		&recordingPipeline{},
	)
	if result.err != nil {
		t.Fatal(result.err)
	}
	for _, want := range []string{
		"installed (dry-run): metabol",
		"[dry-run] no changes were made",
	} {
		if !strings.Contains(result.stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, result.stdout)
		}
	}
	if result.stderr != "" {
		t.Errorf("stderr = %q", result.stderr)
	}
	if _, err := os.Stat(prefix); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dry-run created %q or returned unexpected error: %v", prefix, err)
	}
}

func TestEmbeddedSkill(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	smith, err := newSkillSmith(&stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	skills, err := smith.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 {
		t.Fatalf("len(skills) = %d, want 1", len(skills))
	}
	skill := skills[0]
	if skill.Dir != "metabol" || skill.Name != "metabol" {
		t.Errorf("skill directory/name = %q/%q, want metabol/metabol", skill.Dir, skill.Name)
	}
	if skill.Description == "" {
		t.Error("skill description is empty")
	}
	if !strings.Contains(skill.Body, "## Choose time windows deliberately") {
		t.Errorf("skill body is missing window guidance:\n%s", skill.Body)
	}
}

func TestHelpMentionsSkillsSubcommand(t *testing.T) {
	t.Parallel()
	result := runCLIForTest(
		t,
		context.Background(),
		[]string{"-h"},
		nil,
		nil,
		&recordingPipeline{},
	)
	if !errors.Is(result.err, flag.ErrHelp) {
		t.Fatalf("error = %v, want flag.ErrHelp", result.err)
	}
	if want := "metabol skills <command>"; !strings.Contains(result.stderr, want) {
		t.Errorf("stderr missing %q:\n%s", want, result.stderr)
	}
}
