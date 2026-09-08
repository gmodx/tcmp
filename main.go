package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

const maxTerminalRead = 255

// boundedTTYInput avoids Bubble Tea's 256-byte ambiguous-input boundary for
// unmarked Shift+Insert pastes while preserving the terminal file descriptor.
type boundedTTYInput struct {
	*os.File
}

func (input boundedTTYInput) Read(buffer []byte) (int, error) {
	if len(buffer) > maxTerminalRead {
		buffer = buffer[:maxTerminalRead]
	}
	return input.File.Read(buffer)
}

func main() {
	left, right, fileCount, err := loadArguments(os.Args[1:], os.Stdout, os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "tcomp:", err)
		os.Exit(2)
	}

	cachePath, cachePathErr := sessionCachePath()
	if cachePathErr != nil {
		fmt.Fprintln(os.Stderr, "tcomp: cache disabled:", cachePathErr)
	}
	restored := false
	if cachePathErr == nil {
		left, right, restored, err = restoreCachedSession(left, right, fileCount, cachePath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "tcomp: ignored cached session:", err)
		}
	}

	model := newAppModel(left, right)
	if restored {
		model.status = "Restored the previous left and right text from the session cache."
	}
	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithInput(boundedTTYInput{File: os.Stdin}),
	)
	finalModel, runErr := program.Run()
	if cachePathErr == nil {
		if finalApp, ok := finalModel.(appModel); ok {
			if err := saveSessionCache(cachePath, finalApp.workspace); err != nil {
				fmt.Fprintln(os.Stderr, "tcomp: unable to cache the current text:", err)
			}
		}
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "tcomp: unable to start terminal UI:", runErr)
		os.Exit(1)
	}
}

func loadArguments(arguments []string, standardOutput, errorOutput io.Writer) ([]string, []string, int, error) {
	flags := flag.NewFlagSet("tcomp", flag.ContinueOnError)
	flags.SetOutput(errorOutput)
	showVersion := flags.Bool("version", false, "print the tcomp version and exit")
	flags.Usage = func() {
		fmt.Fprintln(errorOutput, "Usage: tcomp [--version] [left-file] [right-file]")
		fmt.Fprintln(errorOutput, "Open zero, one, or two UTF-8 text files in an editable side-by-side comparison.")
	}
	if err := flags.Parse(arguments); err != nil {
		return nil, nil, 0, err
	}
	if *showVersion {
		fmt.Fprintf(standardOutput, "tcomp %s\n", appVersion)
		return nil, nil, 0, flag.ErrHelp
	}
	if flags.NArg() > 2 {
		flags.Usage()
		return nil, nil, 0, fmt.Errorf("expected at most two file paths")
	}

	documents := [2][]string{{""}, {""}}
	for index, path := range flags.Args() {
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("read %s: %w", path, err)
		}
		if !utf8.Valid(content) {
			return nil, nil, 0, fmt.Errorf("read %s: file is not valid UTF-8", path)
		}
		value := strings.TrimPrefix(string(content), "\ufeff")
		documents[index] = splitText(value)
	}
	return documents[0], documents[1], flags.NArg(), nil
}
