// Command ketju is the message board's binary: one static executable holding
// the server and the offline tools.
//
// Only the storage tools are here so far. docs/09 puts serve, config and the
// rest in Phase 1; dump and fsck are Phase 0 because they are what you reach
// for when the storage is the thing that is wrong, and wanting them for the
// first time during an incident is too late to write them.
package main

import (
	"fmt"
	"os"
)

const usage = `ketju — a message board

usage: ketju <command> [flags]

commands:
  dump   print a thread log as JSON lines, one object per record

Run ketju <command> -h for a command's flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "dump":
		err = runDump(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "ketju: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "ketju: %v\n", err)
		os.Exit(exitCodeFor(err))
	}
}
