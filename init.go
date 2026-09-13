package metabol

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Songmu/prompter"
)

//go:embed metabol.sample.yaml
var sampleConfig []byte

type confirmFunc func(string, bool) bool

func runInit(args []string, outStream, errStream io.Writer) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get current directory: %w", err)
	}
	return runInitAt(args, outStream, errStream, dir, prompter.YN)
}

func runInitAt(
	args []string,
	outStream, errStream io.Writer,
	dir string,
	confirm confirmFunc,
) error {
	fs := flag.NewFlagSet(cmdName+" init", flag.ContinueOnError)
	fs.SetOutput(errStream)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: %s init\n", cmdName)
		fmt.Fprintln(fs.Output(), "\nCreate a sample metabol.yaml in the current directory.")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}

	configPath := filepath.Join(dir, DefaultConfigPath)
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("%s already exists", DefaultConfigPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check %s: %w", DefaultConfigPath, err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read current directory: %w", err)
	}
	if len(entries) > 0 &&
		!confirm("The current directory is not empty. Create metabol.yaml anyway?", false) {
		fmt.Fprintln(outStream, "Initialization canceled.")
		return nil
	}
	if err := os.WriteFile(configPath, sampleConfig, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", DefaultConfigPath, err)
	}
	fmt.Fprintf(outStream, "Created %s.\n\n", DefaultConfigPath)
	fmt.Fprintln(outStream, "Next steps:")
	fmt.Fprintf(outStream, "  1. Edit %s and replace the example source list.\n", DefaultConfigPath)
	fmt.Fprintln(outStream, "  2. Install mdhq if needed: npm install --global @songmu/mdhq")
	fmt.Fprintf(outStream, "  3. Run %s.\n", cmdName)
	return nil
}
