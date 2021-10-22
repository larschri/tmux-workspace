package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// runTmux invokes tmux with the given commands
func runTmux(cmds ...[]string) error {
	var s []string
	for _, c := range cmds {
		s = append(s, c...)
		if s[len(s)-1] != ";" {
			s = append(s, ";")
		}
	}

	out, err := exec.Command("tmux", s...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run tmux command %v (%s) %w", s, string(out), err)
	}

	return nil
}

// paneAttr invokes tmux list-panes to fetch a pane attribute, and returns a slice with an entry for each pane
func paneAttr(attr string) ([]string, error) {
	out, err := exec.Command("tmux", "list-panes", "-F", "#{"+attr+"}").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get attribute %v: %w", attr, err)
	}

	return strings.Split(strings.TrimSpace(string(out)), "\n"), nil
}

// narrowScreenLayout defines a layout intended for "small" screens
func narrowScreenLayout(win string) []string {
	return []string{
		"select-layout", "-t", win, "main-vertical", ";",
		"resize-pane", "-x", "90", "-y", "20", "-t", fmt.Sprintf("%s.%d", win, 1), ";",
		"select-pane", "-t", fmt.Sprintf("%s.%d", win, 0), ";",
	}
}

// wideScreenLayout defines a layout intended for large (4k-ish) screens
func wideScreenLayout(win string) []string {
	return []string{
		"select-layout", "-t", win, "even-horizontal", ";",
		"resize-pane", "-x", "100", "-t", fmt.Sprintf("%s.%d", win, 0), ";",
		"select-pane", "-t", fmt.Sprintf("%s.%d", win, 1), ";",
	}
}

// openWindow creates a new tmux window
func openWindow(session, window, dirname string) ([]string, error) {
	info, err := os.Stat(dirname)
	if err != nil {
		return nil, fmt.Errorf("failed to stat %s: %w", dirname, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", dirname)
	}

	absWin := fmt.Sprintf("%s:%s", session, window)

	if err := runTmux([]string{"has-session", "-t", "" + absWin}); err == nil {
		return nil, fmt.Errorf("session already exists: %s", absWin)
	}

	// TODO: make HISTFILE optional? maybe check if it exists or smth.
	env := "HISTFILE=" + dirname + "/.bash_history"
	newPanes := []string{
		"new-window", "-e", env, "-c", dirname, "-t", session + ":", "-n", window, ";",
		"split-window", "-e", env, "-c", dirname, "-t", absWin, ";",
		"split-window", "-e", env, "-c", dirname, "-t", absWin, ";",
	}

	wwidth, err := paneAttr("window_width")
	if err != nil {
		return nil, err
	}

	if width, err := strconv.Atoi(wwidth[0]); err != nil || width < 300 {
		return append(newPanes, narrowScreenLayout(absWin)...), nil
	}

	return append(newPanes, wideScreenLayout(absWin)...), nil
}

// flipLayout flips between the two layouts (wideScreenLayout/narrowScreenLayout)
func flipLayout(session, window string) ([]string, error) {
	absWin := fmt.Sprintf("%s:%s", session, window)
	pane := absWin + "." + os.Getenv("TMUX_PANE") // TODO: only works for current window

	flipMainPane := []string{
		"swap-pane", "-s", fmt.Sprintf("%s.%d", absWin, 0), "-t", fmt.Sprintf("%s.%d", absWin, 1), ";",
		"select-pane", "-t", pane, ";",
	}

	paneAtBottomAttrs, err := paneAttr("pane_at_bottom")
	if err != nil {
		return nil, err
	}
	if len(paneAtBottomAttrs) != 3 {
		return nil, fmt.Errorf("expected 3 panes, got: %d", strconv.Itoa(len(paneAtBottomAttrs)))
	}

	if paneAtBottomAttrs[1] == "0" {
		return append(flipMainPane, wideScreenLayout(absWin)...), nil
	}

	return append(flipMainPane, narrowScreenLayout(absWin)...), nil
}

// usage prints the usage
func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS] [directory]\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "Create a new workspace by providing a directory, or flip between workspace layouts.\n\n")
	flag.PrintDefaults()
}

// main runs tmux-workspace
func main() {
	flag.Usage = usage
	session := flag.String("session", "", "the target session")
	window := flag.String("window", "", "the target window")
	prnt := flag.Bool("print", false, "print the tmux commands instead of executing")
	flag.Parse()

	if len(flag.Args()) > 1 {
		flag.Usage()
		os.Exit(1)
	}

	if os.Getenv("TMUX") == "" {
		fmt.Fprintf(os.Stderr, "please run inside tmux\n")
		os.Exit(1)
	}

	if *session == "" {
		s, err := paneAttr("session_name")
		if err != nil {
			fmt.Fprintf(os.Stderr, "couldn't find session name: %s\n", err.Error())
			os.Exit(1)
		}
		session = &s[0]
	}

	var commands []string
	var err error
	if len(flag.Args()) == 1 {
		// Create new workspace window for the given directory
		absPath, err := filepath.Abs(flag.Args()[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get absolute path of %s: %w\n", flag.Args()[0], err)
			os.Exit(1)
		}

		if *window == "" {
			p := strings.ReplaceAll(absPath, ".", "_")
			window = &p
		}

		commands, err = openWindow(*session, *window, absPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "open failed: %s\n", err.Error())
			os.Exit(1)
		}
	} else {
		// Flip layout for the given workspace window
		if *window == "" {
			w, err := paneAttr("window_name")
			if err != nil {
				fmt.Fprintf(os.Stderr, "couldn't find window name: %s\n", err.Error())
				os.Exit(1)
			}
			window = &w[0]
		}

		commands, err = flipLayout(*session, *window)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to flip layouts: %s\n", err.Error())
			os.Exit(1)
		}
	}

	if *prnt {
		fmt.Println(strings.Join(commands, " "))
	} else {
		if err := runTmux(commands); err != nil {
			fmt.Fprintf(os.Stderr, "failed to run %v: %s\n", commands, err)
			os.Exit(1)
		}
	}
}
