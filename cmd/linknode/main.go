// Command linknode runs the Link Node: it holds the node's identity, keeps
// a presence connection to the Link.us service, and serves one public
// folder on a local preview address.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/betterbotbureau/linkus/internal/identity"
	"github.com/betterbotbureau/linkus/internal/node"
	"github.com/betterbotbureau/linkus/internal/site"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// command is one `linknode <name>` command.
type command struct {
	name  string
	usage string // the line in the usage text; "" keeps it out of it
	run   func(args []string) error
}

// commands are the CLI's commands, in usage order. Builds with more
// features add theirs from init (see addCommand).
var commands = []command{
	{"run", "run              start the node and keep running", cmdRun},
	{"status", "status           print ID, folders, publish state, server view", cmdStatus},
	{"publish", "publish on|off   put the public folder online, or take it offline", cmdPublish},
	{"folder", "folder [path]    show the public folder, or choose a new one (applies at next start)", cmdFolder},
	{"forget-identity", "forget-identity  give up this address for good (with --yes); the next start gets a new one", cmdForgetIdentity},
	{"example", "example          write the built-in example site into an empty public folder", cmdExample},
	{"public", "public           list exactly what visitors can fetch (and what is hidden)", cmdPublic},
	{"log", "log [-n N]       show the last N logged visits (default 20)", cmdLog},
	{"export", "export <file>    write identity, state and the public folder to one archive", cmdExport},
	{"restore", "restore <file>   restore an export (refuses to replace an identity without --force)", cmdRestore},
	{"version", "version          print the version", cmdVersion},
}

// addCommand adds a command to the usage list after the one named after.
func addCommand(after string, c command) {
	for i := range commands {
		if commands[i].name == after {
			commands = append(commands[:i+1], append([]command{c}, commands[i+1:]...)...)
			return
		}
	}
	commands = append(commands, c)
}

// flagLines are the usage text's flag lines; builds with more features
// add theirs (see extraFlags).
var flagLines = []string{
	"--config        config file             (LINKNODE_CONFIG, default linknode.conf in the data dir, if present)",
	"--server        Link.us service base URL (LINKNODE_SERVER, default https://hub.link.us)",
	"--data-dir      settings and identity   (LINKNODE_DATA_DIR, default ~/Link/Settings)",
	"--public-dir    the public folder       (LINKNODE_PUBLIC_DIR, default ~/Link/Public)",
	"--preview-addr  local preview listener  (LINKNODE_PREVIEW_ADDR, default 127.0.0.1:7465)",
	"--stun          STUN servers, comma-separated (LINKNODE_STUN, default derived from --server)",
	"--portmap       ask the router for a UDP port mapping (LINKNODE_PORTMAP, default true)",
}

// extraFlags registers flags beyond the core ones on every command.
var extraFlags []func(fs *flag.FlagSet, cfg *node.Config)

// afterCommand runs when a command has finished without an error, after
// its cleanup; builds with more features may start another program here.
var afterCommand func()

func finished() {
	if afterCommand != nil {
		afterCommand()
	}
}

func main() {
	// No command means run: on a device, a service starts the binary as is.
	if len(os.Args) < 2 {
		if err := cmdRun(nil); err != nil {
			fmt.Fprintln(os.Stderr, "linknode:", err)
			os.Exit(1)
		}
		finished()
		return
	}
	name := os.Args[1]
	if name == "-h" || name == "--help" || name == "help" {
		usage()
		return
	}
	for _, c := range commands {
		if c.name == name {
			if err := c.run(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "linknode:", err)
				os.Exit(1)
			}
			finished()
			return
		}
	}
	fmt.Fprintf(os.Stderr, "linknode: unknown command %q\n\n", name)
	usage()
	os.Exit(2)
}

// runNote follows "none at all also means run" in the usage text.
var runNote = ""

// aboutLines is what `linknode version` says after the version.
var aboutLines = []string{
	"Link Node publishes a folder on this computer as a website.",
	"Source code on GitHub: https://github.com/betterbotbureau/linkus",
	"Free and open source under the MIT License.",
	"Updates: `linknode status` says when a newer release exists.",
}

func cmdVersion([]string) error {
	fmt.Println("linknode", version)
	for _, l := range aboutLines {
		fmt.Println(l)
	}
	return nil
}

func usage() {
	var b strings.Builder
	b.WriteString("Usage: linknode <command> [flags]\n\nCommands (none at all also means run" + runNote + "):\n")
	for _, c := range commands {
		if c.usage != "" {
			b.WriteString("  " + c.usage + "\n")
		}
	}
	b.WriteString("\nFlags for every command (environment variable in parentheses):\n")
	for _, l := range flagLines {
		b.WriteString("  " + l + "\n")
	}
	b.WriteString(`
A config file holds the same settings as "key = value" lines, by flag name
without the dashes in front (for example: public-dir = /srv/site). Flags
win over environment variables, which win over the config file.
`)
	fmt.Fprint(os.Stderr, b.String())
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func parseConfig(name string, args []string, extra func(*flag.FlagSet)) (node.Config, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	if extra != nil {
		extra(fs)
	}
	var cfg node.Config
	configPath := fs.String("config", envOr("LINKNODE_CONFIG", ""), "config file (default: "+node.ConfigFileName+" in the settings folder, if present)")
	fs.StringVar(&cfg.Server, "server", envOr("LINKNODE_SERVER", ""), "Link.us service base URL")
	fs.StringVar(&cfg.DataDir, "data-dir", envOr("LINKNODE_DATA_DIR", ""), "identity and state directory")
	fs.StringVar(&cfg.PublicDir, "public-dir", envOr("LINKNODE_PUBLIC_DIR", ""), "public folder")
	fs.StringVar(&cfg.PreviewAddr, "preview-addr", envOr("LINKNODE_PREVIEW_ADDR", ""), "local preview address")
	stun := fs.String("stun", envOr("LINKNODE_STUN", ""), "comma-separated STUN servers (default: derived from --server)")
	fs.BoolVar(&cfg.PortMap, "portmap", envOr("LINKNODE_PORTMAP", "true") != "false", "ask the router for a UDP port mapping")
	for _, f := range extraFlags {
		f(fs, &cfg)
	}
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	// The config file fills in whatever flags and the environment left
	// unset. An explicitly named file must exist; the data-directory one
	// is optional.
	if *configPath != "" {
		if err := node.ApplyConfigFile(fs, *configPath, false); err != nil {
			return cfg, err
		}
		cfg.ConfigFile = *configPath
	} else {
		dataDir := cfg.DataDir
		if dataDir == "" {
			// The default settings folder moved to ~/Link/Settings; bring
			// an older node's settings (and identity) along, once.
			if from, err := node.MigrateDataDir(); err != nil {
				fmt.Fprintln(os.Stderr, "linknode: could not move the settings folder:", err)
			} else if from != "" {
				fmt.Fprintf(os.Stderr, "Moved Link Node's settings from %s to ~/Link/Settings.\n", from)
			}
			d, err := node.DefaultDataDir()
			if err != nil {
				return cfg, err
			}
			dataDir = d
		}
		path := filepath.Join(dataDir, node.ConfigFileName)
		if _, err := os.Stat(path); err == nil {
			if err := node.ApplyConfigFile(fs, path, true); err != nil {
				return cfg, err
			}
			cfg.ConfigFile = path
		}
	}
	if *stun != "" {
		for _, s := range strings.Split(*stun, ",") {
			if s = strings.TrimSpace(s); s != "" {
				cfg.STUN = append(cfg.STUN, s)
			}
		}
	}
	if err := cfg.Defaults(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// runEnv is what the desktop app's part of `run` builds on.
type runEnv struct {
	cfg     node.Config
	log     *slog.Logger
	folder  *node.Folder
	id      *identity.Identity
	created bool
	rtc     *node.WebRTC // nil when WebRTC could not start
	updates *node.UpdateWatcher
}

// desktopSession is the desktop app's part of a running node.
type desktopSession interface {
	banner()                                     // its lines after the core's startup lines
	setAddress(addr string)                      // the service named the node's address
	background(ctx context.Context)              // runs once registered, until ctx ends
	foreground(ctx context.Context, stop func()) // owns the foreground (a tray) or returns
}

// onRun runs at the start of `run`, before the agent files are written.
var onRun []func(cfg node.Config)

// statusLines print the desktop app's lines in `linknode status`;
// isRunning tells whether a node runs on these settings.
var (
	statusLines []func(cfg node.Config)
	isRunning   func(cfg node.Config) bool
)

// The desktop app's hooks; nil in the CLI edition. alreadyRunning shows a
// running node's window (a second double-click) and reports whether one
// runs; startDesktop brings up the settings page and the visitor
// features once the core is ready.
var (
	alreadyRunning func(cfg node.Config) bool
	startDesktop   func(env *runEnv) desktopSession
)

func cmdRun(args []string) error {
	cfg, err := parseConfig("run", args, nil)
	if err != nil {
		return err
	}
	if alreadyRunning != nil && alreadyRunning(cfg) {
		return nil
	}
	node.AdoptLinkFolders(&cfg)
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := node.CheckPublicDir(cfg.PublicDir, cfg.DataDir); err != nil {
		return err
	}
	if err := node.EnsurePublicDir(cfg.PublicDir); err != nil {
		return fmt.Errorf("create public folder: %w", err)
	}
	// The live folder handle: everything serves through it, and the owner
	// can re-point it from the settings page while the node runs.
	folder, err := node.OpenPublicFolder(cfg.PublicDir, cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open public folder: %w", err)
	}
	defer folder.Close()

	id, created, restored, err := node.LoadIdentity(cfg.DataDir, log)
	if err != nil {
		return fmt.Errorf("load identity: %w", err)
	}
	switch {
	case created:
		log.Info("new identity created", "data_dir", cfg.DataDir)
	case restored:
		log.Info("identity restored from its backup; the node keeps its address", "data_dir", cfg.DataDir)
	}
	node.EnsureSettingsReadme(cfg.DataDir)
	// A fresh start tries again: this copy runs the node unless another
	// copy is running then.
	if err := node.ClearDisplaced(cfg.DataDir); err != nil {
		log.Warn("could not clear an earlier step-aside", "error", err)
	}
	// A restored node lost its settings, so it starts unpublished like a
	// new one: resetting the settings never puts a site online.
	if err := node.EnsurePublishDefault(cfg.DataDir, created || restored); err != nil {
		log.Warn("could not record the publish state", "error", err)
	}
	// Written with the last address the service gave (no network on
	// startup); refreshed with the service's answer in the background.
	localAddr := ""
	if id.NodeID != "" {
		localAddr = node.SavedAddress(cfg.DataDir)
	}
	for _, f := range onRun {
		f(cfg)
	}
	node.EnsureAgentFiles(cfg.PublicDir, localAddr, cfg.PreviewAddr, log)

	preview, err := node.StartPreview(cfg.PreviewAddr, folder, log)
	if err != nil {
		return fmt.Errorf("start preview: %w (is another Link Node, with a different settings folder, or another program using %s? Use --preview-addr to pick another port)", err, cfg.PreviewAddr)
	}
	defer preview.Close()

	// WebRTC answering serves the same folder under the same rules as the
	// preview. If it cannot start, the node still runs locally: WebRTC
	// failures never affect the preview or presence.
	var visits node.VisitHandler
	rtc, err := node.NewWebRTC(folder, id, cfg.STUN, node.LoadUDPPort(cfg.DataDir), log)
	if err != nil {
		log.Error("webrtc disabled: cannot bind UDP port", "error", err)
		rtc = nil
	} else {
		defer rtc.Close()
		visits = rtc
		rtc.SetMaxVisits(node.LoadMaxVisits(cfg.DataDir))
		rtc.SetRequestLog(node.OpenRequestLog(cfg.DataDir, log))
		// A stable port across runs keeps the router's mapping table
		// to one entry for this node.
		if err := node.SaveUDPPort(cfg.DataDir, rtc.Port()); err != nil {
			log.Warn("could not remember the UDP port", "error", err)
		}
		log.Info("webrtc ready", "udp_port", rtc.Port())
	}
	updates := node.NewUpdateWatcher(version)
	var desk desktopSession
	if startDesktop != nil {
		desk = startDesktop(&runEnv{cfg: cfg, log: log, folder: folder, id: id, created: created, rtc: rtc, updates: updates})
	}
	if startDesktop == nil {
		// The card (the owner's name for the node, or while published the
		// site's title, else "Node <id>") and the check-in for the link.us
		// accounts that see the node. The desktop app starts its own.
		cards := node.OpenNodeCards(cfg.DataDir, id, version, cardNamer(folder.Dir, id), nil)
		if rtc != nil {
			rtc.SetNodeCard(cards)
		}
		go cards.Run(context.Background(), cfg.Server, log)
		accounts := node.NewAccounts(cfg.Server, id, version, log)
		go accounts.Run(context.Background())
	}

	if cfg.ConfigFile != "" {
		fmt.Printf("Config file:    %s\n", cfg.ConfigFile)
	}
	fmt.Printf("Public folder:  %s\n", folder.Dir())
	fmt.Printf("Local preview:  %s\n", node.LocalURL(preview.Addr))
	if id.NodeID != "" {
		fmt.Printf("Node ID:        %s\n", id.NodeID)
	}
	if node.LoadPublished(cfg.DataDir) {
		fmt.Printf("Published:      yes\n")
	} else {
		fmt.Printf("Published:      no — run `linknode publish on` to go live\n")
	}
	if desk != nil {
		desk.banner()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Registration runs first; then the offline-page mirror (independent of
	// publishing — the page is what shows while unpublished) and the publish
	// supervisor, which holds presence and port mapping open exactly while
	// published. The preview works regardless of any of this.
	go func() {
		if id.NodeID == "" {
			nodeID, err := node.RegisterLoop(ctx, cfg.Server, id, log)
			if err != nil {
				return // ctx ended
			}
			log.Info("registered", "id", nodeID)
			fmt.Printf("Node ID:        %s\n", nodeID)
		} else {
			// Tell the service the node still exists. Best effort: offline
			// or refused, the saved ID stays in use. A different reply means
			// an unused registration was reclaimed while the node was off;
			// adopt the new ID before anything uses it.
			cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			got, err := node.Confirm(cctx, cfg.Server, id)
			cancel()
			switch {
			case err != nil:
				log.Warn("could not confirm the node's ID with the service", "error", err)
			case got != id.NodeID:
				log.Warn("this node's unused ID was reclaimed; using its new ID", "old", id.NodeID, "new", got)
				if err := id.SetNodeID(got); err != nil {
					log.Error("store node ID", "error", err)
				}
				fmt.Printf("Node ID:        %s (new)\n", got)
			}
		}
		go node.ConfirmLoop(ctx, cfg.Server, id, log)
		go updates.Run(ctx, cfg.Server, log, func(u node.UpdateInfo) {
			fmt.Printf("Update:         %s is available (this is %s): %s\n", u.Latest, u.Current, u.URL)
			log.Info("update available", "latest", u.Latest, "current", u.Current, "url", u.URL)
		})
		// The server names the canonical address (subdomain form once
		// per-node origins exist); refresh the agent files with it.
		if addr := nodeAddress(cfg, id.NodeID); addr != "" {
			fmt.Printf("Address:        %s\n", addr)
			node.EnsureAgentFiles(cfg.PublicDir, addr, cfg.PreviewAddr, log)
			if desk != nil {
				desk.setAddress(addr)
			}
		}
		go node.WatchOfflinePage(ctx, cfg.Server, id, folder.Dir, cfg.DataDir, log)
		if desk != nil {
			go desk.background(ctx)
		}
		node.PublishSupervisor(ctx, cfg.DataDir, func(sub context.Context) {
			var wg sync.WaitGroup
			if rtc != nil && cfg.PortMap {
				wg.Add(1)
				go func() {
					defer wg.Done()
					node.RunPortMapping(sub, rtc, cfg.STUN, log)
				}()
			}
			if errors.Is(node.PresenceLoop(sub, cfg.Server, id, visits, log), node.ErrInUse) {
				// Another copy of this node is running; the supervisor
				// sees the mark and keeps this one offline.
				if err := node.MarkDisplaced(cfg.DataDir); err != nil {
					log.Error("could not record that another copy is running", "error", err)
				}
			}
			wg.Wait()
		}, log)
	}()

	if desk != nil {
		desk.foreground(ctx, stop)
	}
	<-ctx.Done()
	log.Info("shutting down")
	for _, f := range atShutdown {
		f()
	}
	return nil
}

// atShutdown runs, in order, when a running node stops: features that
// must stop something of theirs before the process exits add to it.
var atShutdown []func()

func cmdStatus(args []string) error {
	cfg, err := parseConfig("status", args, nil)
	if err != nil {
		return err
	}

	id, err := identity.Load(cfg.DataDir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		fmt.Println("Identity:       none yet (run `linknode run` first)")
	case err != nil:
		return err
	case id.NodeID == "":
		fmt.Println("Node ID:        not registered yet")
	default:
		fmt.Printf("Node ID:        %s\n", id.NodeID)
	}
	fmt.Printf("Public folder:  %s\n", cfg.PublicDir)
	fmt.Printf("Settings folder: %s\n", cfg.DataDir)
	if cfg.ConfigFile != "" {
		fmt.Printf("Config file:    %s\n", cfg.ConfigFile)
	} else {
		fmt.Printf("Config file:    none (optional: %s)\n", filepath.Join(cfg.DataDir, node.ConfigFileName))
	}
	fmt.Printf("Local preview:  %s\n", node.LocalURL(cfg.PreviewAddr))

	if node.LoadPublished(cfg.DataDir) {
		fmt.Println("Published:      yes")
	} else {
		fmt.Println("Published:      no")
	}
	if displaced, since := node.Displaced(cfg.DataDir); displaced {
		when := ""
		if !since.IsZero() {
			when = " since " + since.Local().Format("2006-01-02 15:04")
		}
		fmt.Printf("Active here:    no — another copy of this node was already running%s;\n", when)
		fmt.Println("                this one stays offline. To run it here: quit the other copy, then restart Link Node here.")
	}
	for _, f := range statusLines {
		f(cfg)
	}
	if _, err := os.Stat(filepath.Join(cfg.PublicDir, node.OfflineFileName)); err == nil {
		fmt.Println("Offline page:   configured (offline.html)")
	} else {
		fmt.Println("Offline page:   none (visitors see the plain offline notice)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if rel, err := node.CheckRelease(ctx, cfg.Server); err != nil {
		fmt.Printf("Version:        %s (could not check for updates)\n", version)
	} else if u := node.Compare(version, rel); u.Available {
		fmt.Printf("Version:        %s; update available: %s, %s\n", version, u.Latest, u.URL)
	} else {
		fmt.Printf("Version:        %s\n", version)
	}

	if id == nil || id.NodeID == "" {
		fmt.Printf("Server:         %s\n", cfg.Server)
		return nil
	}
	st, err := node.Status(ctx, cfg.Server, id.NodeID)
	switch {
	case err != nil:
		fmt.Printf("Server:         %s — unreachable (%v)\n", cfg.Server, err)
	case st.Online:
		fmt.Printf("Server:         %s — node online\n", cfg.Server)
	default:
		// Link.us gives the day only (UTC).
		fmt.Printf("Server:         %s — node offline, last seen on %s\n", cfg.Server, st.LastSeen.UTC().Format("2006-01-02"))
	}
	if err == nil {
		addr := st.Address
		if addr == "" {
			addr = node.PublicURL(cfg.Server, id.NodeID)
		}
		fmt.Printf("Address:        %s\n", addr)
	}
	return nil
}

// nodeAddress returns the node's public URL as the service names it: its
// per-node subdomain, or — when the service answers without one, which
// means it runs without per-node subdomains — the path form under the
// service. While the service is unreachable it is the last address the
// service gave; the node never invents one (the service's own host is not
// a node address). "" when not registered or never told.
func nodeAddress(cfg node.Config, nodeID string) string {
	if nodeID == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, err := node.Status(ctx, cfg.Server, nodeID)
	if err != nil {
		return node.SavedAddress(cfg.DataDir)
	}
	addr := st.Address
	if addr == "" {
		addr = node.PublicURL(cfg.Server, nodeID)
	}
	node.SaveAddress(cfg.DataDir, addr) //nolint:errcheck // a cache; the next start asks again
	return addr
}

func cmdPublish(args []string) error {
	if len(args) < 1 || (args[0] != "on" && args[0] != "off") {
		return errors.New("usage: linknode publish on|off")
	}
	on := args[0] == "on"
	cfg, err := parseConfig("publish", args[1:], nil)
	if err != nil {
		return err
	}
	if _, err := identity.Load(cfg.DataDir); err != nil {
		return fmt.Errorf("no node here yet (run `linknode run` first): %w", err)
	}
	if err := node.SavePublished(cfg.DataDir, on); err != nil {
		return err
	}
	if on {
		fmt.Println("Published. A running node goes online within seconds;")
		fmt.Println("otherwise it will be online the next time it runs.")
	} else {
		fmt.Println("Unpublished. A running node goes offline within seconds.")
		fmt.Println("The local preview keeps working.")
	}
	return nil
}

// cmdFolder shows the public folder, or saves a new choice — the same
// choice the settings page's "Change folder…" makes.
func cmdFolder(args []string) error {
	var target string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		target, args = args[0], args[1:]
	}
	cfg, err := parseConfig("folder", args, nil)
	if err != nil {
		return err
	}
	if target == "" {
		fmt.Println(cfg.PublicDir)
		return nil
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if err := node.CheckPublicDir(abs, cfg.DataDir); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	if err := node.EnsurePublicDir(abs); err != nil {
		return fmt.Errorf("create public folder: %w", err)
	}
	if err := node.SavePublicDir(cfg.DataDir, abs); err != nil {
		return err
	}
	fmt.Printf("Public folder set to %s.\n", abs)
	if cfg.PublicDirPinned {
		fmt.Println("Note: public-dir is also set by a flag, LINKNODE_PUBLIC_DIR or the")
		fmt.Println("config file, and that setting wins over this choice — change it there.")
		return nil
	}
	fmt.Println("A running node serves it after a restart (as a service:")
	fmt.Println("`sudo systemctl restart linknode@$USER`).")
	return nil
}

// cmdForgetIdentity deletes the node's identity and its backup, so the
// next start gets a new ID and address. Irreversible, hence --yes.
func cmdForgetIdentity(args []string) error {
	yes := false
	rest := args[:0:0]
	for _, a := range args {
		if a == "--yes" || a == "-yes" {
			yes = true
			continue
		}
		rest = append(rest, a)
	}
	cfg, err := parseConfig("forget-identity", rest, nil)
	if err != nil {
		return err
	}
	if !yes {
		id, _ := identity.Load(cfg.DataDir)
		addr := "this node's address"
		if id != nil && id.NodeID != "" {
			addr = "node " + id.NodeID
		}
		return fmt.Errorf("this deletes the node's key for good: %s is lost and the next start gets a new address. If you are sure, run: linknode forget-identity --yes (stop the node first; `linknode export` keeps a backup)", addr)
	}
	if isRunning != nil && isRunning(cfg) {
		return errors.New("Link Node is running; quit it first")
	}
	if err := node.ForgetIdentity(cfg.DataDir); err != nil {
		return err
	}
	fmt.Println("Identity deleted. The next start creates a new identity and a new address.")
	return nil
}

// cmdExample writes the built-in example site into the public folder.
func cmdExample(args []string) error {
	cfg, err := parseConfig("example", args, nil)
	if err != nil {
		return err
	}
	if err := node.CheckPublicDir(cfg.PublicDir, cfg.DataDir); err != nil {
		return err
	}
	if err := node.EnsurePublicDir(cfg.PublicDir); err != nil {
		return fmt.Errorf("create public folder: %w", err)
	}
	if err := node.WriteExampleSite(cfg.PublicDir); err != nil {
		if errors.Is(err, node.ErrHasHomepage) {
			return fmt.Errorf("%s already has an index.html; the example is only written into a folder without a homepage", cfg.PublicDir)
		}
		return err
	}
	fmt.Printf("Example site written to %s.\n", cfg.PublicDir)
	fmt.Println("Existing files were left alone. A running node serves it right away.")
	return nil
}

func cmdPublic(args []string) error {
	cfg, err := parseConfig("public", args, nil)
	if err != nil {
		return err
	}
	st, err := site.Open(cfg.PublicDir)
	if err != nil {
		return fmt.Errorf("open public folder: %w", err)
	}
	defer st.Close()
	served, skipped, err := st.List()
	if err != nil {
		return err
	}
	fmt.Printf("Public folder: %s\n\n", cfg.PublicDir)
	if len(served) == 0 {
		fmt.Println("Nothing is served yet: the folder has no servable files.")
	} else {
		fmt.Println("Visitors can fetch:")
		var total int64
		for _, e := range served {
			fmt.Printf("  %-50s %10d bytes\n", e.Path, e.Size)
			total += e.Size
		}
		fmt.Printf("  %d files, %d bytes in total\n", len(served), total)
	}
	if len(skipped) > 0 {
		fmt.Println("\nIn the folder but never served:")
		for _, s := range skipped {
			fmt.Printf("  %-50s %s\n", s.Path, s.Reason)
		}
	}
	return nil
}

func cmdLog(args []string) error {
	var n int
	cfg, err := parseConfig("log", args, func(fs *flag.FlagSet) {
		fs.IntVar(&n, "n", 20, "how many visits to show")
	})
	if err != nil {
		return err
	}
	entries, err := node.TailRequestLog(cfg.DataDir, n)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println("No visits logged yet.")
		return nil
	}
	for _, e := range entries {
		paths := strings.Join(e.Paths, " ")
		if paths == "" {
			paths = "-"
		}
		ip := e.IP
		if ip == "" {
			ip = "-"
		}
		fmt.Printf("%s  %-16s %-15s ice=%-12s %6.1fs %8d bytes  %s\n",
			e.Time.Local().Format("2006-01-02 15:04:05"), e.Outcome, ip, e.ICE, e.Secs, e.Bytes, paths)
	}
	return nil
}

func cmdExport(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return errors.New("usage: linknode export <file.tar.gz>")
	}
	out := args[0]
	cfg, err := parseConfig("export", args[1:], nil)
	if err != nil {
		return err
	}
	if _, err := identity.Load(cfg.DataDir); err != nil {
		return fmt.Errorf("nothing to export here: %w", err)
	}
	f, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := node.Export(f, cfg.DataDir, cfg.PublicDir); err != nil {
		f.Close()
		os.Remove(out)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Printf("Exported to %s\n", out)
	fmt.Println("This archive contains the node's PRIVATE KEY. Anyone holding it")
	fmt.Println("can act as this node. Store it like a password.")
	return nil
}

func cmdRestore(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return errors.New("usage: linknode restore <file.tar.gz> [--force]")
	}
	in := args[0]
	var force bool
	cfg, err := parseConfig("restore", args[1:], func(fs *flag.FlagSet) {
		fs.BoolVar(&force, "force", false, "replace an existing identity")
	})
	if err != nil {
		return err
	}
	f, err := os.Open(in)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.PublicDir, 0o755); err != nil {
		return err
	}
	if err := node.Restore(f, cfg.DataDir, cfg.PublicDir, force); err != nil {
		return err
	}
	fmt.Printf("Restored into:\n  data:   %s\n  public: %s\n", cfg.DataDir, cfg.PublicDir)
	fmt.Println("Run `linknode run` to bring the node up with its old identity.")
	return nil
}
