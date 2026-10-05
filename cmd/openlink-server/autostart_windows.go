package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const taskName = "OpenLink Server"

// runAutostart manages a Task Scheduler task that starts the agent when the
// user logs on. A logon task runs in the user's own session, with their Steam
// install and profile, which the game server needs. A Windows service would
// run in session 0 as another account, where the game is not expected to work.
func runAutostart(c config, args []string) error {
	sub := "status"
	if len(args) > 1 {
		sub = args[1]
	}
	switch sub {
	case "enable":
		if c.path == "" {
			return errors.New("autostart needs a config file so the agent starts with your settings:\n" +
				"  1. openlink-server [your flags] init-config\n  2. openlink-server autostart enable")
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		exe, _ = filepath.Abs(exe)
		tr := fmt.Sprintf(`"%s" -config "%s"`, exe, c.path)
		out, err := exec.Command("schtasks", "/Create", "/TN", taskName, "/TR", tr,
			"/SC", "ONLOGON", "/RL", "LIMITED", "/F").CombinedOutput()
		if err != nil {
			return fmt.Errorf("schtasks: %v: %s", err, strings.TrimSpace(string(out)))
		}
		fmt.Printf("OpenLink Server will start when you log on, using %s.\n", c.path)
		return nil
	case "disable":
		out, err := exec.Command("schtasks", "/Delete", "/TN", taskName, "/F").CombinedOutput()
		if err != nil {
			return fmt.Errorf("schtasks: %v: %s", err, strings.TrimSpace(string(out)))
		}
		fmt.Println("Autostart removed.")
		return nil
	case "status":
		out, err := exec.Command("schtasks", "/Query", "/TN", taskName, "/V", "/FO", "LIST").CombinedOutput()
		if err != nil {
			fmt.Println("Autostart is off.")
			return nil
		}
		for _, line := range strings.Split(string(out), "\n") {
			for _, k := range []string{"TaskName:", "Status:", "Task To Run:", "Last Run Time:", "Last Result:"} {
				if strings.HasPrefix(strings.TrimSpace(line), k) {
					fmt.Println(strings.TrimSpace(line))
				}
			}
		}
		return nil
	}
	return fmt.Errorf("usage: autostart enable|disable|status")
}
