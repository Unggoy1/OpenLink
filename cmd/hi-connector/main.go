// hi-connector runs on a player's PC. It lists community servers and, for the
// chosen one, re-advertises the server's own LAN beacon locally and forwards
// the game's UDP 1343 traffic to the server. The game sees an ordinary LAN game.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"halocommunity/connect"
	"halocommunity/internal/api"
	"halocommunity/internal/directory"
	"halocommunity/internal/game"
	"halocommunity/internal/sim"
)

// version is set at build time (-ldflags "-X main.version=v0.1.0").
var version = "dev"

const usage = `hi-connector — join community Halo Infinite servers

usage:
  hi-connector [flags] list               list servers
  hi-connector [flags] join <id|name>     bring a server onto your LAN browser
  hi-connector [flags] probe <id|name>    check that a host answers probes (proxy mode or -simulate)
  hi-connector version

flags:
`

func main() {
	fs := flag.NewFlagSet("hi-connector", flag.ExitOnError)
	dirURL := fs.String("directory", envOr("HICOMM_DIRECTORY", "http://127.0.0.1:8080"), "directory URL (env HICOMM_DIRECTORY)")
	install := fs.String("install", "", "game install root, used to check the build; empty = search common locations")
	all := fs.Bool("all", false, "list: include servers of other builds")
	mode := fs.String("advertise", "loopback", "join: how to show the server to the game: loopback (127.0.0.1, proven on Windows and Linux) or broadcast (LAN IP)")
	lanIP := fs.String("lan-ip", "", "join: local IPv4 to use in broadcast mode; empty = the default-route address")
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage); fs.PrintDefaults() }
	fs.Parse(os.Args[1:])
	args := fs.Args()
	if len(args) == 0 {
		fs.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	dc := directory.NewClient(*dirURL, "")
	localBuild := ""
	if in, err := game.FindInstall(*install); err == nil {
		localBuild, _ = in.Build()
	}

	var err error
	switch args[0] {
	case "version":
		fmt.Println(version)
		return
	case "list":
		err = list(ctx, dc, localBuild, *all)
	case "join", "probe":
		if len(args) < 2 {
			fs.Usage()
			os.Exit(2)
		}
		var s api.ServerInfo
		if s, err = find(ctx, dc, args[1]); err == nil {
			if args[0] == "probe" {
				err = probe(ctx, s)
			} else {
				err = join(ctx, *dirURL, s, localBuild, *mode, *lanIP)
			}
		}
	default:
		fs.Usage()
		os.Exit(2)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func list(ctx context.Context, dc *directory.Client, localBuild string, all bool) error {
	filter := localBuild
	if all {
		filter = ""
	}
	servers, err := dc.List(ctx, filter)
	if err != nil {
		return err
	}
	if localBuild == "" {
		fmt.Println("(game install not found: showing all builds; pass -install to filter)")
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tREGION\tSTATUS\tJOINABLE\tBUILD")
	for _, s := range servers {
		build := s.Build
		if localBuild != "" && s.Build != localBuild {
			build += " (mismatch)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%v\t%s\n", s.ID, s.Name, s.Region, s.Status, s.Joinable, build)
	}
	tw.Flush()
	if len(servers) == 0 {
		fmt.Println("no servers listed")
	}
	return nil
}

// find matches an exact ID first, then a unique case-insensitive name prefix.
func find(ctx context.Context, dc *directory.Client, key string) (api.ServerInfo, error) {
	servers, err := dc.List(ctx, "")
	if err != nil {
		return api.ServerInfo{}, err
	}
	var hits []api.ServerInfo
	for _, s := range servers {
		if s.ID == key {
			return s, nil
		}
		if strings.HasPrefix(strings.ToLower(s.Name), strings.ToLower(key)) {
			hits = append(hits, s)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return api.ServerInfo{}, fmt.Errorf("no server matches %q", key)
	default:
		return api.ServerInfo{}, fmt.Errorf("%q matches %d servers; use the ID", key, len(hits))
	}
}

func resolve(s api.ServerInfo) (*net.UDPAddr, error) {
	return net.ResolveUDPAddr("udp", net.JoinHostPort(s.Host, strconv.Itoa(s.Port)))
}

func probe(ctx context.Context, s api.ServerInfo) error {
	addr, err := resolve(s)
	if err != nil {
		return err
	}
	fmt.Printf("probing %s (%s) at %s ...\n", s.Name, s.ID, addr)
	res, err := sim.Probe(ctx, addr, 5, 2*time.Second)
	if err != nil {
		return err
	}
	fmt.Printf("%d/%d echoes", res.Received, res.Sent)
	if len(res.RTTs) > 0 {
		var sum time.Duration
		for _, r := range res.RTTs {
			sum += r
		}
		fmt.Printf(", average round trip %v", (sum / time.Duration(len(res.RTTs))).Round(time.Millisecond))
	}
	fmt.Println()
	if res.Received == 0 {
		fmt.Println("no echo: the host is not in proxy mode (or -simulate), or UDP", s.Port, "is not reachable (port forward / firewall)")
	}
	return nil
}

func join(ctx context.Context, dirURL string, s api.ServerInfo, localBuild, mode, lanIP string) error {
	switch {
	case localBuild == "":
		fmt.Printf("note: game install not found, build not checked (pass -install <folder with version.txt>)\n  server build: %s\n", s.Build)
	case s.Build != localBuild:
		fmt.Printf("warning: server build %s differs from yours (%s); the game will not list or join it\n", s.Build, localBuild)
	default:
		fmt.Printf("build matches: %s\n", localBuild)
	}
	sess, err := connect.Start(s, connect.Options{Directory: dirURL, Mode: mode, LANIP: lanIP})
	if err != nil {
		return err
	}
	defer sess.Stop()
	st := sess.Status()
	fmt.Printf("joining %s (%s)\n  server   %s\n  local    %s (%s)\n", s.Name, s.ID,
		net.JoinHostPort(s.Host, strconv.Itoa(s.Port)), st.Local, st.Mode)
	fmt.Println("open Halo Infinite > Custom Games > Server; the host appears under its PC name within a few seconds. Ctrl+C to stop.")

	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nstopped")
			return nil
		case <-t.C:
		}
		st := sess.Status()
		age := "none yet"
		if st.BeaconAge >= 0 {
			age = st.BeaconAge.Round(time.Second).String()
		}
		line := fmt.Sprintf("beacon age %-8s adverts %-5d  to server %d pkts %s  from server %d pkts %s",
			age, st.Adverts, st.UpPackets, kb(st.UpBytes), st.DownPackets, kb(st.DownBytes))
		if st.Err != "" {
			line += "  [" + st.Err + "]"
		}
		fmt.Println(line)
	}
}

func kb(n uint64) string { return fmt.Sprintf("%.1f KB", float64(n)/1024) }
