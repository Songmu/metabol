package thresh

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

const cmdName = "thresh"

type pipelineRunner interface {
	Run(context.Context, PipelineRequest, io.Writer, io.Writer) error
}

// Run the thresh
func Run(ctx context.Context, argv []string, outStream, errStream io.Writer) error {
	return run(ctx, argv, outStream, errStream, time.Now, os.LookupEnv, nil)
}

func run(
	ctx context.Context,
	argv []string,
	outStream, errStream io.Writer,
	now func() time.Time,
	lookupEnv LookupEnvFunc,
	pipeline pipelineRunner,
) error {
	fs := flag.NewFlagSet(
		fmt.Sprintf("%s (v%s rev:%s)", cmdName, version, revision), flag.ContinueOnError)
	fs.SetOutput(errStream)
	ver := fs.Bool("version", false, "display version")
	var cli CLIValues
	cli.RegisterFlags(fs)
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *ver {
		return printVersion(outStream)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}

	config, err := LoadResolvedConfig(cli, lookupEnv)
	if err != nil {
		return err
	}
	windows, err := SelectDailyWindows(
		config.Location,
		config.Daily,
		now(),
		config.At,
		config.WindowCount,
	)
	if err != nil {
		return fmt.Errorf("select processing windows: %w", err)
	}

	sources := make([]string, len(config.Sources))
	for i, source := range config.Sources {
		sources[i] = source.URL
	}
	if pipeline == nil {
		pipeline = NewPipeline(RSSnipFetcher{}, NewMDHQ(nil))
	}
	var failures []error
	for _, window := range windows {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		if err := pipeline.Run(ctx, PipelineRequest{
			Sources: sources,
			Since:   window.Start,
			Until:   window.End,
			Root:    config.Root,
			Assets:  config.Assets,
			Update:  config.Update,
		}, outStream, errStream); err != nil {
			failures = append(failures, err)
			if ctx.Err() != nil {
				break
			}
		}
	}
	if err := errors.Join(failures...); err != nil {
		return &reportedError{err: err}
	}
	return nil
}

func printVersion(out io.Writer) error {
	_, err := fmt.Fprintf(out, "%s v%s (rev:%s)\n", cmdName, version, revision)
	return err
}

// reportedError marks a failure whose details were already written to stderr
// while the run progressed. It keeps the exit status non-zero and preserves
// errors.Is and errors.As without printing every detail a second time.
type reportedError struct {
	err error
}

func (e *reportedError) Error() string {
	return "completed with errors; see the log above"
}

func (e *reportedError) Unwrap() error {
	return e.err
}
