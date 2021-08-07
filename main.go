package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/mickael-menu/zk/internal/cli"
	"github.com/mickael-menu/zk/internal/cli/cmd"
	"github.com/mickael-menu/zk/internal/core"
	executil "github.com/mickael-menu/zk/internal/util/exec"
)

var Version = "dev"
var Build = "dev"

var root struct {
	Init  cmd.Init  `cmd group:"zk" help:"Create a new notebook in the given directory."`
	Index cmd.Index `cmd group:"zk" help:"Index the notes to be searchable."`

	New  cmd.New  `cmd group:"notes" help:"Create a new note in the given notebook directory."`
	List cmd.List `cmd group:"notes" help:"List notes matching the given criteria."`
	Edit cmd.Edit `cmd group:"notes" help:"Edit notes matching the given criteria."`

	NotebookDir string  `type:path placeholder:PATH help:"Turn off notebook auto-discovery and set manually the notebook where commands are run."`
	WorkingDir  string  `short:W type:path placeholder:PATH help:"Run as if zk was started in <PATH> instead of the current working directory."`
	NoInput     NoInput `help:"Never prompt or ask for confirmation."`

	ShowHelp ShowHelp         `cmd hidden default:"1"`
	LSP      cmd.LSP          `cmd hidden`
	Version  kong.VersionFlag `hidden help:"Print zk version."`
}

// NoInput is a flag preventing any user prompt when enabled.
type NoInput bool

func (f NoInput) BeforeApply(container *cli.Container) error {
	container.Terminal.NoInput = true
	return nil
}

// ShowHelp is the default command run. It's equivalent to `zk --help`.
type ShowHelp struct{}

func (cmd *ShowHelp) Run(container *cli.Container) error {
	parser, err := kong.New(&root, options(container)...)
	if err != nil {
		return err
	}
	ctx, err := parser.Parse([]string{"--help"})
	if err != nil {
		return err
	}
	return ctx.Run(container)
}

func main() {
	args := os.Args[1:]

	// Create the dependency graph.
	container, err := cli.NewContainer(Version)
	fatalIfError(err)

	// Open the notebook if there's any.
	dirs, args, err := parseDirs(args)
	fatalIfError(err)
	searchDirs, err := notebookSearchDirs(dirs)
	fatalIfError(err)
	err = container.SetCurrentNotebook(searchDirs)
	fatalIfError(err)

	// Run the alias or command.
	if isAlias, err := runAlias(container, args); isAlias {
		fatalIfError(err)
	} else {
		parser, err := kong.New(&root, options(container)...)
		fatalIfError(err)
		ctx, err := parser.Parse(args)
		fatalIfError(err)

		// Index the current notebook except if the user is running the `index`
		// command, otherwise it would hide the stats.
		if ctx.Command() != "index" {
			if notebook, err := container.CurrentNotebook(); err == nil {
				_, err = notebook.Index(false)
				ctx.FatalIfErrorf(err)
			}
		}

		err = ctx.Run(container)
		ctx.FatalIfErrorf(err)
	}
}

func options(container *cli.Container) []kong.Option {
	term := container.Terminal
	return []kong.Option{
		kong.Bind(container),
		kong.Name("zk"),
		kong.UsageOnError(),
		kong.HelpOptions{
			Compact:        true,
			FlagsLast:      true,
			WrapUpperBound: 100,
		},
		kong.Vars{
			"version": "zk " + strings.TrimPrefix(Version, "v"),
		},
		kong.Groups(map[string]string{
			"filter": "Filtering",
			"sort":   "Sorting",
			"format": "Formatting",
			"notes":  term.MustStyle("NOTES", core.StyleYellow, core.StyleBold) + "\n" + term.MustStyle("Edit or browse your notes", core.StyleBold),
			"zk":     term.MustStyle("NOTEBOOK", core.StyleYellow, core.StyleBold) + "\n" + term.MustStyle("A notebook is a directory containing a collection of notes", core.StyleBold),
		}),
	}
}

func fatalIfError(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "zk: error: %v\n", err)
		os.Exit(1)
	}
}

// runAlias will execute a user alias if the command is one of them.
func runAlias(container *cli.Container, args []string) (bool, error) {
	if len(args) < 1 {
		return false, nil
	}

	runningAlias := os.Getenv("ZK_RUNNING_ALIAS")
	for alias, cmdStr := range container.Config.Aliases {
		if alias == runningAlias || alias != args[0] {
			continue
		}

		// Prevent infinite loop if an alias calls itself.
		os.Setenv("ZK_RUNNING_ALIAS", alias)

		// Move to the current notebook's root directory before running the alias.
		if notebook, err := container.CurrentNotebook(); err == nil {
			cmdStr = `cd "` + notebook.Path + `" && ` + cmdStr
		}

		cmd := executil.CommandFromString(cmdStr, args[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			if err, ok := err.(*exec.ExitError); ok {
				os.Exit(err.ExitCode())
				return true, nil
			} else {
				return true, err
			}
		}
		return true, nil
	}

	return false, nil
}

// notebookSearchDirs returns the places where zk will look for a notebook.
// The first successful candidate will be used as the working directory from
// which path arguments are relative from.
//
// By order of precedence:
//   1. --notebook-dir flag
//   2. current working directory
//   3. ZK_NOTEBOOK_DIR environment variable
func notebookSearchDirs(dirs cli.Dirs) ([]cli.Dirs, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	// 1. --notebook-dir flag
	if dirs.NotebookDir != "" {
		// If --notebook-dir is used, we want to only check there to report
		// "notebook not found" errors.
		if dirs.WorkingDir == "" {
			dirs.WorkingDir = wd
		}
		return []cli.Dirs{dirs}, nil
	}

	candidates := []cli.Dirs{}

	// 2. current working directory
	wdDirs := dirs
	if wdDirs.WorkingDir == "" {
		wdDirs.WorkingDir = wd
	}
	wdDirs.NotebookDir = wdDirs.WorkingDir
	candidates = append(candidates, wdDirs)

	// 3. ZK_NOTEBOOK_DIR environment variable
	if notebookDir, ok := os.LookupEnv("ZK_NOTEBOOK_DIR"); ok {
		dirs := dirs
		dirs.NotebookDir = notebookDir
		if dirs.WorkingDir == "" {
			dirs.WorkingDir = notebookDir
		}
		candidates = append(candidates, dirs)
	}

	return candidates, nil
}

// parseDirs returns the paths specified with the --notebook-dir and
// --working-dir flags.
//
// We need to parse these flags before Kong, because we might need it to
// resolve zk command aliases before parsing the CLI.
func parseDirs(args []string) (cli.Dirs, []string, error) {
	var d cli.Dirs
	var err error

	findFlag := func(long string, short string, args []string) (string, []string, error) {
		newArgs := []string{}

		foundFlag := ""
		for i, arg := range args {
			if arg == long || (short != "" && arg == short) {
				foundFlag = arg
			} else if foundFlag != "" {
				newArgs = append(newArgs, args[i+1:]...)
				path, err := filepath.Abs(arg)
				return path, newArgs, err
			} else {
				newArgs = append(newArgs, arg)
			}
		}
		if foundFlag != "" {
			return "", newArgs, errors.New(foundFlag + " requires a path argument")
		}
		return "", newArgs, nil
	}

	d.NotebookDir, args, err = findFlag("--notebook-dir", "", args)
	if err != nil {
		return d, args, err
	}
	d.WorkingDir, args, err = findFlag("--working-dir", "-W", args)
	if err != nil {
		return d, args, err
	}

	return d, args, nil
}
