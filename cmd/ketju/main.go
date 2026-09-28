// Command ketju is the message board's binary: one static executable holding
// the server and the offline tools.
//
// dump and fsck came first, in Phase 0, because they are what you reach for
// when the storage is the thing that is wrong, and wanting them for the first
// time during an incident is too late to write them. serve is the server, and
// reindex rebuilds what the database derives from the logs. config check is
// what an operator runs before a restart.
package main

import (
	"fmt"
	"os"
)

const usage = `ketju — a message board

usage: ketju <command> [flags]

commands:
  serve    run the server
  dump     print a thread log as JSON lines, one object per record
  fsck     walk every log, verify every checksum, rebuild every index
  reindex  rebuild the database's projection tables from the thread logs
  admin    administer members from the command line (admin promote)
  config   check the config file and print the effective configuration

Run ketju <command> -h for a command's flags.
`

// buildCommands are commands only some builds have: the e2e build adds its
// Playwright server here (e2e.go). A release build has none.
var buildCommands = map[string]func(args []string) error{}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "serve":
		err = runServe(os.Args[2:])
	case "dump":
		err = runDump(os.Args[2:])
	case "fsck":
		err = runFsck(os.Args[2:])
	case "admin":
		err = runAdmin(os.Args[2:])
	case "reindex":
		err = runReindex(os.Args[2:])
	case "config":
		err = runConfig(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		run, ok := buildCommands[os.Args[1]]
		if !ok {
			fmt.Fprintf(os.Stderr, "ketju: unknown command %q\n\n%s", os.Args[1], usage)
			os.Exit(2)
		}
		err = run(os.Args[2:])
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "ketju: %v\n", err)
		os.Exit(exitCodeFor(err))
	}
}
