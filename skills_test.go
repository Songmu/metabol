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
	"time"
)

func TestRunSkillsList(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := run(
		context.Background(),
		[]string{"skills", "list"},
		&stdout,
		&stderr,
		func() time.Time { return time.Time{} },
		func(string) (string, bool) { return "", false },
		&recordingPipeline{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); !strings.Contains(got, "metabol") ||
		!strings.Contains(got, "collect articles") {
		t.Errorf("stdout = %q", got)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunSkillsUsage(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := run(
		context.Background(),
		[]string{"skills"},
		&stdout,
		&stderr,
		func() time.Time { return time.Time{} },
		func(string) (string, bool) { return "", false },
		&recordingPipeline{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q", stdout.String())
	}
	for _, want := range []string{
		"Usage: metabol skills <command> [options]",
		"install",
		"status",
		"uninstall",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr.String())
		}
	}
}

func TestRunSkillsInstallDryRun(t *testing.T) {
	t.Parallel()
	prefix := filepath.Join(t.TempDir(), "skills")
	var stdout, stderr bytes.Buffer
	err := run(
		context.Background(),
		[]string{"skills", "install", "--dry-run", "--prefix", prefix},
		&stdout,
		&stderr,
		func() time.Time { return time.Time{} },
		func(string) (string, bool) { return "", false },
		&recordingPipeline{},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"installed (dry-run): metabol",
		"[dry-run] no changes were made",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q", stderr.String())
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
	var stdout, stderr bytes.Buffer
	err := run(
		context.Background(),
		[]string{"-h"},
		&stdout,
		&stderr,
		func() time.Time { return time.Time{} },
		func(string) (string, bool) { return "", false },
		&recordingPipeline{},
	)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("error = %v, want flag.ErrHelp", err)
	}
	if want := "metabol skills <command>"; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr missing %q:\n%s", want, stderr.String())
	}
}
