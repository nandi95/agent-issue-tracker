package ait

import (
	"context"
	"fmt"
	"strings"
)

// Command describes a single CLI subcommand.
type Command struct {
	Name      string
	Summary   string   // one-liner for global help
	Usage     string   // flag summary for global help, e.g. "--title <t> [--type]"
	UsageCont string   // optional continuation line for wide usage
	Help      string   // full per-command help text (hand-written, with examples)
	Flags     []string // flag names for completion scripts
	NeedsDB   bool     // false for help, version, completion
	Run       func(a *App, ctx context.Context, args []string) error
}

var commands []Command
var commandsByName map[string]*Command
var subcommandHelp map[string]string

func init() {
	commands = registerCommands()
	commandsByName = make(map[string]*Command, len(commands))
	for i := range commands {
		commandsByName[commands[i].Name] = &commands[i]
	}
	subcommandHelp = registerSubcommandHelp()
}

// LookupCommand finds a command by name.
func LookupCommand(name string) (*Command, bool) {
	cmd, ok := commandsByName[name]
	return cmd, ok
}

// CommandNames returns all command names in registration order.
func CommandNames() []string {
	names := make([]string, len(commands))
	for i, cmd := range commands {
		names[i] = cmd.Name
	}
	return names
}

// CommandFlags returns the flag names for a command, or nil if not found.
func CommandFlags(name string) []string {
	cmd, ok := commandsByName[name]
	if !ok {
		return nil
	}
	return cmd.Flags
}

func generateHelpText() string {
	var b strings.Builder
	b.WriteString("Usage: ait [--db <path>] <command> [options]\n\nCommands:\n")

	for _, cmd := range commands {
		if cmd.Usage != "" {
			fmt.Fprintf(&b, "  %-10s %-35s %s\n", cmd.Name, cmd.Usage, cmd.Summary)
		} else {
			fmt.Fprintf(&b, "  %-10s %-35s %s\n", cmd.Name, "", cmd.Summary)
		}
		if cmd.UsageCont != "" {
			fmt.Fprintf(&b, "  %-10s %s\n", "", cmd.UsageCont)
		}
	}

	b.WriteString(`
Global options:
  --db <path>     Use a specific database file (default: .ait/ait.db)
  --version       Show version and check for updates
`)
	return b.String()
}

// PrintHelp prints the global help text generated from the command registry.
func PrintHelp() {
	fmt.Print(generateHelpText())
}

// PrintCommandHelp prints help for a specific command, falling back to global help.
func PrintCommandHelp(cmd string) {
	c, ok := commandsByName[cmd]
	if ok && c.Help != "" {
		fmt.Print(c.Help)
		return
	}
	if text, ok := subcommandHelp[cmd]; ok {
		fmt.Print(text)
		return
	}
	PrintHelp()
}
