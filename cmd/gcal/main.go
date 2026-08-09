package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/DeJayDev/kirigo/internal/configenv"
	"github.com/DeJayDev/kirigo/internal/gcal"
	"github.com/DeJayDev/kirigo/internal/googlecal"
	"github.com/DeJayDev/kirigo/internal/output"
	"google.golang.org/api/option"
)

// outputFormat is resolved once in run() and used by emit/fail.
var outputFormat = "json"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if err := configenv.LoadDefault(); err != nil {
		_ = output.WriteError(os.Stderr, err.Error(), "json")
		return 2
	}
	// Global flags precede the subcommand (git-style): `gcal -format toon list ...`.
	gfs := flag.NewFlagSet("gcal", flag.ContinueOnError)
	format := output.RegisterFlag(gfs)
	if err := gfs.Parse(args); err != nil {
		return 2
	}
	f, err := output.ResolveFormat(*format)
	if err != nil {
		_ = output.WriteError(os.Stderr, err.Error(), "json")
		return 2
	}
	outputFormat = f
	rest := gfs.Args()
	if len(rest) == 0 {
		return usage()
	}
	cmd, rest := rest[0], rest[1:]
	switch cmd {
	case "setup":
		return cmdSetup(rest)
	case "calendars":
		return cmdCalendars(rest)
	case "list":
		return cmdList(rest)
	case "get":
		return cmdGet(rest)
	case "create":
		return cmdCreate(rest)
	case "update":
		return cmdUpdate(rest)
	case "delete":
		return cmdDelete(rest)
	case "quickadd":
		return cmdQuickAdd(rest)
	case "freebusy":
		return cmdFreeBusy(rest)
	case "log":
		return cmdLog(rest)
	case "restore":
		return cmdRestore(rest)
	case "prune":
		return cmdPrune(rest)
	default:
		return fail(&gcal.ValidationError{Msg: fmt.Sprintf("unknown command %q", cmd)})
	}
}

func usage() int {
	fmt.Fprintln(os.Stderr, "usage: gcal [-format json|toon] <setup|calendars|list|get|create|update|delete|quickadd|freebusy|log|restore|prune>")
	return 2
}

func cmdSetup(args []string) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	account := fs.String("account", gcal.DefaultAccount, "account name")
	port := fs.Int("port", 0, "localhost callback port (default 8765)")
	paste := fs.Bool("paste", false, "skip the local server; paste the redirected URL instead")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if os.Getenv("GOOGLE_OAUTH_CLIENT_ID") == "" || os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET") == "" {
		return fail(&gcal.ValidationError{Msg: "GOOGLE_OAUTH_CLIENT_ID and GOOGLE_OAUTH_CLIENT_SECRET are required (create a Desktop OAuth client in Google Cloud Console)"})
	}
	tokenPath, err := gcal.TokenPath(*account)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := googlecal.RunSetup(ctx, googlecal.SetupOptions{
		ClientID:     os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
		TokenPath:    tokenPath,
		Port:         *port,
		Paste:        *paste,
	}); err != nil {
		return fail(err)
	}
	return emit(map[string]string{"status": "ok", "account": *account, "token_path": tokenPath})
}

func cmdCalendars(args []string) int {
	fs := flag.NewFlagSet("calendars", flag.ContinueOnError)
	account := fs.String("account", "", "account name")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	app, err := newApp(ctx, *account)
	if err != nil {
		return fail(err)
	}
	res, err := app.Calendars(ctx)
	if err != nil {
		return fail(err)
	}
	return emit(res)
}

func cmdList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	account := fs.String("account", "", "account name")
	var cals stringSlice
	fs.Var(&cals, "calendar", "calendar id (repeatable; default primary)")
	from := fs.String("from", "now", "window start")
	to := fs.String("to", "in 7 days", "window end")
	query := fs.String("q", "", "full-text filter")
	max := fs.Int64("max", 0, "max events (0 = all)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	app, err := newApp(ctx, *account)
	if err != nil {
		return fail(err)
	}
	res, err := app.List(ctx, cals, *from, *to, *query, *max)
	if err != nil {
		return fail(err)
	}
	return emit(res)
}

func cmdGet(args []string) int {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	account := fs.String("account", "", "account name")
	cal := fs.String("calendar", "", "calendar id (default primary)")
	raw := fs.Bool("raw", false, "emit the full Google event resource")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	id := fs.Arg(0)
	if id == "" {
		return fail(&gcal.ValidationError{Msg: "get requires an <event-id>"})
	}
	if err := extraArgs(fs, "<event-id>"); err != nil {
		return fail(err)
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	app, err := newApp(ctx, *account)
	if err != nil {
		return fail(err)
	}
	res, err := app.Get(ctx, *cal, id, *raw)
	if err != nil {
		return fail(err)
	}
	return emit(res)
}

func cmdCreate(args []string) int {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	account := fs.String("account", "", "account name")
	cal := fs.String("calendar", "", "target calendar id (default primary)")
	title := fs.String("title", "", "event title")
	start := fs.String("start", "", "start time")
	end := fs.String("end", "", "end time")
	desc := fs.String("description", "", "event description")
	jsonPath := fs.String("json", "", "read a full event body from a file or - for stdin")
	dryRun := fs.Bool("dry-run", false, "print the change without applying it")
	raw := fs.Bool("raw", false, "emit the full Google event resource")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	r, closeFn, err := jsonReader(*jsonPath)
	if err != nil {
		return fail(err)
	}
	defer closeFn()
	ctx, cancel := ctxTimeout()
	defer cancel()
	app, err := newApp(ctx, *account)
	if err != nil {
		return fail(err)
	}
	res, err := app.Create(ctx, gcal.CreateParams{
		Calendar: *cal, Title: *title, Start: *start, End: *end,
		Description: *desc, JSON: r, DryRun: *dryRun, Raw: *raw,
	})
	if err != nil {
		return fail(err)
	}
	return emit(res)
}

func cmdUpdate(args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	account := fs.String("account", "", "account name")
	cal := fs.String("calendar", "", "calendar id (default primary)")
	title := fs.String("title", "", "event title")
	start := fs.String("start", "", "start time")
	end := fs.String("end", "", "end time")
	desc := fs.String("description", "", "event description")
	jsonPath := fs.String("json", "", "read a partial event body from a file or - for stdin")
	scope := fs.String("scope", "instance", "recurring scope: instance, following, or all")
	dryRun := fs.Bool("dry-run", false, "print the change without applying it")
	raw := fs.Bool("raw", false, "emit the full Google event resource")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	id := fs.Arg(0)
	if id == "" {
		return fail(&gcal.ValidationError{Msg: "update requires an <event-id>"})
	}
	if err := extraArgs(fs, "<event-id>"); err != nil {
		return fail(err)
	}
	r, closeFn, err := jsonReader(*jsonPath)
	if err != nil {
		return fail(err)
	}
	defer closeFn()
	ctx, cancel := ctxTimeout()
	defer cancel()
	app, err := newApp(ctx, *account)
	if err != nil {
		return fail(err)
	}
	res, err := app.Update(ctx, gcal.UpdateParams{
		Calendar: *cal, EventID: id, Title: *title, Start: *start, End: *end,
		Description: *desc, JSON: r, Scope: *scope, DryRun: *dryRun, Raw: *raw,
	})
	if err != nil {
		return fail(err)
	}
	return emit(res)
}

func cmdDelete(args []string) int {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	account := fs.String("account", "", "account name")
	cal := fs.String("calendar", "", "calendar id (default primary)")
	scope := fs.String("scope", "instance", "recurring scope: instance, following, or all")
	confirm := fs.Bool("confirm", false, "confirm the deletion")
	dryRun := fs.Bool("dry-run", false, "print the change without applying it")
	raw := fs.Bool("raw", false, "emit the full Google event resource")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	id := fs.Arg(0)
	if id == "" {
		return fail(&gcal.ValidationError{Msg: "delete requires an <event-id>"})
	}
	if err := extraArgs(fs, "<event-id>"); err != nil {
		return fail(err)
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	app, err := newApp(ctx, *account)
	if err != nil {
		return fail(err)
	}
	res, err := app.Delete(ctx, gcal.DeleteParams{
		Calendar: *cal, EventID: id, Scope: *scope, Confirm: *confirm, DryRun: *dryRun, Raw: *raw,
	})
	if err != nil {
		return fail(err)
	}
	return emit(res)
}

func cmdQuickAdd(args []string) int {
	fs := flag.NewFlagSet("quickadd", flag.ContinueOnError)
	account := fs.String("account", "", "account name")
	cal := fs.String("calendar", "", "target calendar id (default primary)")
	dryRun := fs.Bool("dry-run", false, "print the change without applying it")
	raw := fs.Bool("raw", false, "emit the full Google event resource")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	text := strings.Join(fs.Args(), " ")
	ctx, cancel := ctxTimeout()
	defer cancel()
	app, err := newApp(ctx, *account)
	if err != nil {
		return fail(err)
	}
	res, err := app.QuickAdd(ctx, *cal, text, *dryRun, *raw)
	if err != nil {
		return fail(err)
	}
	return emit(res)
}

func cmdFreeBusy(args []string) int {
	fs := flag.NewFlagSet("freebusy", flag.ContinueOnError)
	account := fs.String("account", "", "account name")
	var cals stringSlice
	fs.Var(&cals, "calendar", "calendar id (repeatable; default primary)")
	from := fs.String("from", "now", "window start")
	to := fs.String("to", "in 7 days", "window end")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	app, err := newApp(ctx, *account)
	if err != nil {
		return fail(err)
	}
	res, err := app.FreeBusy(ctx, cals, *from, *to)
	if err != nil {
		return fail(err)
	}
	return emit(res)
}

func cmdLog(args []string) int {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	account := fs.String("account", "", "account name")
	max := fs.Int("max", 0, "max ops (0 = all)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	app, err := storeApp(*account)
	if err != nil {
		return fail(err)
	}
	res, err := app.Log(*max)
	if err != nil {
		return fail(err)
	}
	return emit(res)
}

func cmdRestore(args []string) int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	account := fs.String("account", "", "account name")
	last := fs.Bool("last", false, "restore the most recent op")
	dryRun := fs.Bool("dry-run", false, "print the change without applying it")
	raw := fs.Bool("raw", false, "emit the full Google event resource")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	app, err := newApp(ctx, *account)
	if err != nil {
		return fail(err)
	}
	if err := extraArgs(fs, "<op-id>"); err != nil {
		return fail(err)
	}
	res, err := app.Restore(ctx, fs.Arg(0), *last, *dryRun, *raw)
	if err != nil {
		return fail(err)
	}
	return emit(res)
}

func cmdPrune(args []string) int {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	account := fs.String("account", "", "account name")
	before := fs.String("before", "", "prune ops older than this time")
	all := fs.Bool("all", false, "prune all ops")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	app, err := storeApp(*account)
	if err != nil {
		return fail(err)
	}
	res, err := app.Prune(*before, *all)
	if err != nil {
		return fail(err)
	}
	return emit(res)
}

// newApp builds a fully-authenticated App (needs OAuth env + a stored token).
func newApp(ctx context.Context, accountFlag string) (*gcal.App, error) {
	account, err := gcal.ResolveAccount(accountFlag)
	if err != nil {
		return nil, err
	}
	clientID := os.Getenv("GOOGLE_OAUTH_CLIENT_ID")
	clientSecret := os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		return nil, &gcal.ValidationError{Msg: "GOOGLE_OAUTH_CLIENT_ID and GOOGLE_OAUTH_CLIENT_SECRET are required"}
	}
	tokenPath, err := gcal.TokenPath(account)
	if err != nil {
		return nil, err
	}
	ts, err := googlecal.TokenSource(ctx, googlecal.OAuthConfig(clientID, clientSecret, ""), tokenPath)
	if err != nil {
		return nil, err
	}
	client, err := googlecal.NewClient(ctx, option.WithTokenSource(ts))
	if err != nil {
		return nil, err
	}
	store, err := gcal.NewStore(account)
	if err != nil {
		return nil, err
	}
	return gcal.NewApp(client, store, time.Now), nil
}

// storeApp builds an App for the backup-only commands (log/prune) with no API client.
func storeApp(accountFlag string) (*gcal.App, error) {
	account, err := gcal.ResolveAccount(accountFlag)
	if err != nil {
		return nil, err
	}
	store, err := gcal.NewStore(account)
	if err != nil {
		return nil, err
	}
	return gcal.NewApp(nil, store, time.Now), nil
}

func ctxTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func emit(v any) int {
	if err := output.Write(os.Stdout, v, outputFormat); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write output: %v\n", err)
		return 1
	}
	return 0
}

func fail(err error) int {
	code := 1
	var ve *gcal.ValidationError
	if errors.As(err, &ve) {
		code = 2
	}
	_ = output.WriteError(os.Stderr, err.Error(), outputFormat)
	return code
}

// extraArgs rejects positional args past the first, so a flag placed after the
// positional (e.g. `get <id> --raw`) errors instead of being silently ignored —
// Go's flag package stops at the first non-flag, so flags must come first.
func extraArgs(fs *flag.FlagSet, noun string) error {
	if fs.NArg() > 1 {
		return &gcal.ValidationError{Msg: fmt.Sprintf("unexpected argument %q; flags must come before the %s", fs.Arg(1), noun)}
	}
	return nil
}

func jsonReader(path string) (io.Reader, func(), error) {
	if path == "" {
		return nil, func() {}, nil
	}
	if path == "-" {
		return os.Stdin, func() {}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, func() {}, err
	}
	return f, func() { f.Close() }, nil
}

type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}
