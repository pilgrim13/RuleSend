package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/pilgrim13/rulesend/internal/agent"
	"github.com/pilgrim13/rulesend/internal/core"
	"github.com/pilgrim13/rulesend/internal/hub"
)

const usage = `RuleSend · Your AI rules. Every device.

  rulesend version
  rulesend hub init  --data DIR
  rulesend hub serve --data DIR [--listen 127.0.0.1:8787] [--public-url https://...]
                     [--tls-cert FILE --tls-key FILE]
  rulesend agent enroll --data DIR --hub URL --name NAME --token-file FILE|-
  rulesend agent bind   --data DIR --pack ID --adapter codex|claude|markdown
                       (--path /absolute/file.md | --default) [--label NAME]
  rulesend agent once   --data DIR
  rulesend agent run    --data DIR
  rulesend agent status --data DIR

Enrollment and binding are separate. No application files are changed until
once/run applies a published version. Credentials are read from files or stdin.
Alpha Agents: macOS / Linux. https://github.com/pilgrim13/rulesend
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "RuleSend:", err)
		os.Exit(1)
	}
}
func flags(name string) (*flag.FlagSet, *string) {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	return f, f.String("data", "", "private data directory (required)")
}
func parse(f *flag.FlagSet, data *string, args []string) error {
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected positional argument")
	}
	if *data == "" {
		return errors.New("--data is required")
	}
	return nil
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Print(usage)
		return nil
	}
	if args[0] == "version" {
		fmt.Println("RuleSend", core.Version)
		return nil
	}
	if len(args) < 2 {
		return errors.New("choose hub or agent and a command; see --help")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	switch args[0] + " " + args[1] {
	case "hub init":
		f, data := flags("hub init")
		if err := parse(f, data, args[2:]); err != nil {
			return err
		}
		store, err := hub.Open(*data)
		if err != nil {
			return err
		}
		defer store.Close()
		p, _ := filepath.Abs(filepath.Join(*data, "admin-token"))
		fmt.Printf("Hub initialized. Administrator token is stored privately at:\n%s\nEnter its contents in the web login. Do not paste it into chat or issues.\n", p)
		return nil
	case "hub serve":
		f, data := flags("hub serve")
		listen := f.String("listen", "127.0.0.1:8787", "listen address")
		public := f.String("public-url", "", "exact public HTTPS origin")
		cert := f.String("tls-cert", "", "TLS certificate file")
		key := f.String("tls-key", "", "TLS private key file")
		if err := parse(f, data, args[2:]); err != nil {
			return err
		}
		if err := validateListen(*listen, *public, *cert, *key); err != nil {
			return err
		}
		store, err := hub.Open(*data)
		if err != nil {
			return err
		}
		defer store.Close()
		handler, err := hub.NewServer(store, *public)
		if err != nil {
			return err
		}
		server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
		go func() {
			<-ctx.Done()
			stop, c := context.WithTimeout(context.Background(), 5*time.Second)
			defer c()
			_ = server.Shutdown(stop)
		}()
		scheme := "http"
		if *cert != "" {
			scheme = "https"
		}
		fmt.Printf("RuleSend Hub listening at %s://%s\n", scheme, *listen)
		if *cert != "" {
			err = server.ListenAndServeTLS(*cert, *key)
		} else {
			err = server.ListenAndServe()
		}
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case "agent enroll":
		f, data := flags("agent enroll")
		base := f.String("hub", "", "Hub origin")
		name := f.String("name", "", "friendly device name")
		tokenFile := f.String("token-file", "", "enrollment token file or - for stdin")
		if err := parse(f, data, args[2:]); err != nil {
			return err
		}
		token, err := readToken(*tokenFile)
		if err != nil {
			return err
		}
		id, err := agent.Enroll(ctx, *data, *base, *name, token)
		if err != nil {
			return err
		}
		fmt.Printf("Device enrolled: %s\nNext: bind an explicit local target. No application files changed.\n", id)
		return nil
	case "agent bind":
		f, data := flags("agent bind")
		pack := f.String("pack", "", "pack ID")
		adapter := f.String("adapter", "", "codex, claude, or markdown")
		path := f.String("path", "", "absolute file path")
		def := f.Bool("default", false, "explicitly approve the adapter's default global file")
		label := f.String("label", "", "friendly target label (sent to Hub)")
		if err := parse(f, data, args[2:]); err != nil {
			return err
		}
		if (*path == "") == !*def {
			return errors.New("choose exactly one of --path or --default")
		}
		if *def {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			switch *adapter {
			case "codex":
				if os.Getenv("CODEX_HOME") != "" {
					return errors.New("CODEX_HOME is customized; approve its actual rules file explicitly with --path")
				}
				*path = filepath.Join(home, ".codex", "AGENTS.md")
			case "claude":
				if os.Getenv("CLAUDE_CONFIG_DIR") != "" {
					return errors.New("Claude configuration location is customized; approve its actual rules file explicitly with --path")
				}
				*path = filepath.Join(home, ".claude", "CLAUDE.md")
			default:
				return errors.New("--default requires codex or claude")
			}
		}
		a, err := agent.Open(*data)
		if err != nil {
			return err
		}
		defer a.Close()
		t, err := a.Bind(*pack, *adapter, *path, *label)
		if err != nil {
			return err
		}
		fmt.Printf("Binding saved: %s\nTarget: %s\nPack: %s · adapter: %s\nThe existing file has not changed. Its contents will be preserved outside the new RuleSend block. Run agent once to apply.\n", t.ID, t.Path, t.PackID, t.Adapter)
		return nil
	case "agent once", "agent run", "agent status":
		f, data := flags(args[0] + " " + args[1])
		if err := parse(f, data, args[2:]); err != nil {
			return err
		}
		if args[1] == "status" {
			status, err := agent.ReadStatus(*data)
			if err != nil {
				return err
			}
			fmt.Print(status)
			return nil
		}
		a, err := agent.Open(*data)
		if err != nil {
			return err
		}
		defer a.Close()
		report := func(receipts []core.Receipt, err error) {
			for _, r := range receipts {
				fmt.Printf("%s · v%d · %s\n", r.TargetLabel, r.Version, r.Status)
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, "Agent:", err)
			}
		}
		if args[1] == "run" {
			return a.Run(ctx, report)
		}
		receipts, err := a.Step(ctx, true)
		report(receipts, nil)
		return err
	default:
		return errors.New("unknown command; see rulesend --help")
	}
}

func readToken(path string) (string, error) {
	if path == "" {
		return "", errors.New("--token-file is required")
	}
	var r io.Reader = os.Stdin
	var f *os.File
	if path != "-" {
		st, err := os.Lstat(path)
		if err != nil || !st.Mode().IsRegular() {
			return "", errors.New("token must be supplied in a regular file")
		}
		if st.Mode().Perm()&0077 != 0 {
			return "", errors.New("token file must have owner-only permissions (chmod 600)")
		}
		f, err = os.Open(path)
		if err != nil {
			return "", errors.New("cannot open token file")
		}
		defer f.Close()
		r = f
	}
	b, err := io.ReadAll(io.LimitReader(r, 1025))
	if err != nil || len(b) > 1024 {
		return "", errors.New("cannot read token within size limit")
	}
	return strings.TrimSpace(string(b)), nil
}

func validateListen(addr, public, cert, key string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return errors.New("listen address must be host:port")
	}
	ip := net.ParseIP(host)
	loopback := host == "localhost" || (ip != nil && ip.IsLoopback())
	if (cert == "") != (key == "") {
		return errors.New("provide both TLS certificate and key")
	}
	if !loopback && cert == "" {
		return errors.New("plaintext Hub must bind to loopback; use TLS for remote listening")
	}
	if public != "" {
		u, err := url.Parse(public)
		if err != nil || u.Host == "" || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return errors.New("public URL must be an HTTPS origin")
		}
	}
	if !loopback && public == "" {
		return errors.New("remote TLS listening requires an explicit --public-url")
	}
	return nil
}
