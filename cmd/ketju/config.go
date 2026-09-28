package main

import (
	"encoding"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/terva-sh/ketju/internal/config"
	"github.com/terva-sh/ketju/internal/settings"
)

const configUsage = `usage: ketju config <subcommand> [flags]

subcommands:
  check  validate the config file and print the effective configuration
`

const configCheckUsage = `usage: ketju config check [--config PATH] [--json]

Loads the config file exactly as ketju serve would, applying every KETJU_*
variable in this environment, and reports what the server would run with.
Run it before a restart, and paste its output into a support thread: no
secret value is printed, only whether one is set.

The config file is --config, else $KETJU_CONFIG, else ./config.toml.

When the file is valid, every key this version understands is printed with
its effective value and where the value came from: file, env (a KETJU_*
variable), env-file (a KETJU_*_FILE variable), secret-file (a provider's
client_secret_file), or default. Then come the warnings, the environment
variables this build reads, and the runtime settings, which live in the
database and are never read from the file.

When it is not, every problem is printed, each naming its key.

A warning names something legal that an operator should choose knowingly,
such as fsync = "never". It is printed and does not change the exit code.

exit codes:
  0  the configuration is valid, with or without warnings
  1  the configuration has problems; each is named by its key
  2  usage
  4  the config file does not exist or could not be read

`

// errConfig is a config file that loads but does not validate. It exits 1,
// the code the other commands give a failure in what they were pointed at.
type errConfig struct{ err error }

func (e errConfig) Error() string { return e.err.Error() }
func (e errConfig) Unwrap() error { return e.err }

func runConfig(args []string) error {
	return configMain(os.Stdout, os.Stderr, args, os.Getenv)
}

func configMain(out, errOut io.Writer, args []string, getenv func(string) string) error {
	if len(args) == 0 {
		fmt.Fprint(errOut, configUsage)
		return errUsage{errors.New("config needs a subcommand")}
	}
	switch args[0] {
	case "check":
		return configCheck(out, errOut, args[1:], getenv)
	case "-h", "--help", "help":
		fmt.Fprint(out, configUsage)
		return nil
	default:
		fmt.Fprint(errOut, configUsage)
		return errUsage{fmt.Errorf("unknown config subcommand %q", args[0])}
	}
}

// The --json report. Every value in it is rendered by encoding/json from
// the config's own types, so a secret goes through Secret.MarshalJSON.
type checkReport struct {
	File      string          `json:"file"`
	Valid     bool            `json:"valid"`
	Error     string          `json:"error,omitempty"`
	Problems  []checkProblem  `json:"problems,omitempty"`
	Warnings  []string        `json:"warnings"`
	Keys      []checkKey      `json:"keys,omitempty"`
	Overrides []checkOverride `json:"overrides,omitempty"`
	Settings  []checkSetting  `json:"settings,omitempty"`
}

type checkProblem struct {
	Key string `json:"key"`
	Msg string `json:"message"`
}

type checkKey struct {
	Key    string `json:"key"`
	Value  any    `json:"value"`
	Source string `json:"source"`
	From   string `json:"from,omitempty"`
}

type checkOverride struct {
	Env string `json:"env"`
	Key string `json:"key"`
	Set bool   `json:"set"`
}

type checkSetting struct {
	Name    string          `json:"name"`
	Default json.RawMessage `json:"default"`
	Doc     string          `json:"doc"`
}

func configCheck(out, errOut io.Writer, args []string, getenv func(string) string) error {
	fs_ := flag.NewFlagSet("config check", flag.ContinueOnError)
	fs_.SetOutput(errOut)
	pathFlag := fs_.String("config", "", "config file")
	asJSON := fs_.Bool("json", false, "print the report as JSON instead of text")
	fs_.Usage = func() {
		fmt.Fprint(fs_.Output(), configCheckUsage)
		fs_.PrintDefaults()
	}
	if err := fs_.Parse(args); err != nil {
		return errUsage{err}
	}
	if fs_.NArg() != 0 {
		fs_.Usage()
		return errUsage{errors.New("config check takes no positional arguments")}
	}

	path := configPath(*pathFlag, getenv)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	cfg, warnings, err := config.Load(path, getenv)
	rep := checkReport{File: path, Valid: err == nil, Warnings: warnings}
	if rep.Warnings == nil {
		rep.Warnings = []string{}
	}

	var invalid *config.Error
	switch {
	case errors.As(err, &invalid):
		for _, p := range invalid.Problems {
			rep.Problems = append(rep.Problems, checkProblem{Key: p.Key, Msg: p.Msg})
		}
		err = errConfig{err}
	case err != nil:
		// The file is missing or unreadable: nothing was checked. Exit 4,
		// as the other commands do for data they could not read.
		rep.Error = err.Error()
	default:
		for _, e := range cfg.Entries() {
			rep.Keys = append(rep.Keys, checkKey{Key: e.Key, Value: e.Value,
				Source: string(e.Source.Layer), From: e.Source.From})
		}
		for _, o := range cfg.Overrides() {
			rep.Overrides = append(rep.Overrides, checkOverride{Env: o.Env, Key: o.Key,
				Set: getenv(o.Env) != "" || getenv(o.Env+config.FileSuffix) != ""})
		}
		for _, s := range settings.All() {
			rep.Settings = append(rep.Settings, checkSetting{Name: s.Name,
				Default: json.RawMessage(s.Default), Doc: s.Doc})
		}
	}

	if *asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if werr := enc.Encode(rep); werr != nil {
			return errors.Join(err, werr)
		}
		return err
	}
	if err != nil {
		// main prints the error, which for a config that does not validate
		// is every problem, each on its own line and named by its key.
		for _, w := range warnings {
			fmt.Fprintf(errOut, "warning: %s\n", w)
		}
		return err
	}
	return printCheck(out, rep)
}

// printCheck writes the text report. Values are rendered by formatValue,
// which reaches a secret only through its own MarshalText.
func printCheck(out io.Writer, rep checkReport) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s is valid\n", rep.File)
	if n := len(rep.Warnings); n > 0 {
		fmt.Fprintf(&b, "# with %d warning%s, listed below\n", n, plural(n))
	}
	b.WriteString("#\n# key = value  # where the value came from\n\n")

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, k := range rep.Keys {
		src := k.Source
		if k.From != "" {
			src += " " + k.From
		}
		fmt.Fprintf(tw, "%s = %s\t# %s\n", k.Key, formatValue(k.Value), src)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if len(rep.Warnings) > 0 {
		b.WriteString("\n# warnings\n")
		for _, w := range rep.Warnings {
			fmt.Fprintf(&b, "#   %s\n", w)
		}
	}

	b.WriteString("\n# environment variables this build reads; * is set here.\n")
	fmt.Fprintf(&b, "# Each can instead be given as NAME%s, naming a file that holds the value.\n", config.FileSuffix)
	tw = tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, o := range rep.Overrides {
		mark := " "
		if o.Set {
			mark = "*"
		}
		fmt.Fprintf(tw, "# %s %s\t%s\n", mark, o.Env, o.Key)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	b.WriteString("\n# runtime settings: stored in the database and changed in the admin UI,\n")
	b.WriteString("# never read from this file. Shown with the value a new forum starts with.\n")
	tw = tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, s := range rep.Settings {
		fmt.Fprintf(tw, "#   %s = %s\t# %s\n", s.Name, s.Default, s.Doc)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	_, err := io.WriteString(out, b.String())
	return err
}

// formatValue renders one value as TOML would write it.
//
// A type that marshals itself as text is asked to, before anything else is
// tried, and a Secret is such a type: this is the only route a secret takes
// to the output, and it yields [redacted]. Nothing in this file reads a
// secret's value.
func formatValue(v any) string {
	switch v := v.(type) {
	case encoding.TextMarshaler:
		b, err := v.MarshalText()
		if err != nil {
			return strconv.Quote("(unprintable: " + err.Error() + ")")
		}
		return strconv.Quote(string(b))
	case string:
		return strconv.Quote(v)
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case []string:
		q := make([]string, len(v))
		for i, s := range v {
			q[i] = strconv.Quote(s)
		}
		return "[" + strings.Join(q, ", ") + "]"
	case map[string]string:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = strconv.Quote(k) + " = " + strconv.Quote(v[k])
		}
		if len(parts) == 0 {
			return "{}"
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	default:
		// A type added to Config without a case here. %v goes through
		// String, so even this route redacts a secret.
		return fmt.Sprint(v)
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
