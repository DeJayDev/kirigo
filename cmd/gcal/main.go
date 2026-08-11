package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/alecthomas/kong"

	"github.com/DeJayDev/kirigo/internal/configenv"
	"github.com/DeJayDev/kirigo/internal/gcal"
	"github.com/DeJayDev/kirigo/internal/googlecal"
	"github.com/DeJayDev/kirigo/internal/output"
	"google.golang.org/api/option"
)

type CLI struct {
	Format string `help:"output format: json (default) or toon; overrides KIRIGO_FORMAT"`

	Setup     SetupCmd     `cmd:"" help:"authorize an account via OAuth"`
	Calendars CalendarsCmd `cmd:"" help:"list accessible calendars"`
	List      ListCmd      `cmd:"" help:"list events in a window"`
	Get       GetCmd       `cmd:"" help:"fetch one event"`
	Create    CreateCmd    `cmd:"" help:"create an event"`
	Update    UpdateCmd    `cmd:"" help:"update an event"`
	Delete    DeleteCmd    `cmd:"" help:"delete an event"`
	Quickadd  QuickAddCmd  `cmd:"" help:"create an event from natural-language text"`
	Freebusy  FreeBusyCmd  `cmd:"" help:"query busy/free windows"`
	Log       LogCmd       `cmd:"" help:"show past mutations"`
	Restore   RestoreCmd   `cmd:"" help:"restore a past mutation"`
	Prune     PruneCmd     `cmd:"" help:"remove snapshots"`
}

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if err := configenv.LoadDefault(); err != nil {
		_ = output.WriteError(os.Stderr, err.Error(), "json")
		return 2
	}
	var cli CLI
	parser, err := kong.New(&cli, kong.Name("gcal"),
		kong.Description("Google Calendar event CRUD with restorable snapshots."))
	if err != nil {
		_ = output.WriteError(os.Stderr, err.Error(), "json")
		return 2
	}
	kctx, err := parser.Parse(args)
	if err != nil {
		_ = output.WriteError(os.Stderr, err.Error(), "json")
		return 2
	}
	format, err := output.ResolveFormat(cli.Format)
	if err != nil {
		_ = output.WriteError(os.Stderr, err.Error(), "json")
		return 2
	}
	if err := kctx.Run(&deps{format: format}); err != nil {
		_ = output.WriteError(os.Stderr, err.Error(), format)
		if _, ok := errors.AsType[*gcal.ValidationError](err); ok {
			return 2
		}
		return 1
	}
	return 0
}

// deps is bound into every command's Run; emit writes the result in the resolved format.
type deps struct{ format string }

func (d *deps) emit(res any, err error) error {
	if err != nil {
		return err
	}
	return output.Write(os.Stdout, res, d.format)
}

type SetupCmd struct {
	Account string `default:"default" help:"account name"`
	Port    int    `help:"localhost callback port (default 8765)"`
	Paste   bool   `help:"skip the local server; paste the redirected URL instead"`
}

func (c *SetupCmd) Run(d *deps) error {
	if os.Getenv("GOOGLE_OAUTH_CLIENT_ID") == "" || os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET") == "" {
		return &gcal.ValidationError{Msg: "GOOGLE_OAUTH_CLIENT_ID and GOOGLE_OAUTH_CLIENT_SECRET are required (create a Desktop OAuth client in Google Cloud Console)"}
	}
	tokenPath, err := gcal.TokenPath(c.Account)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := googlecal.RunSetup(ctx, googlecal.SetupOptions{
		ClientID:     os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
		TokenPath:    tokenPath,
		Port:         c.Port,
		Paste:        c.Paste,
	}); err != nil {
		return err
	}
	return d.emit(map[string]string{"status": "ok", "account": c.Account, "token_path": tokenPath}, nil)
}

type CalendarsCmd struct {
	Account string `help:"account name"`
}

func (c *CalendarsCmd) Run(d *deps) error {
	ctx, cancel := ctxTimeout()
	defer cancel()
	app, err := newApp(ctx, c.Account)
	if err != nil {
		return err
	}
	return d.emit(app.Calendars(ctx))
}

type ListCmd struct {
	Account  string   `help:"account name"`
	Calendar []string `help:"calendar id (repeatable; default primary)"`
	From     string   `default:"now" help:"window start"`
	To       string   `default:"in 7 days" help:"window end"`
	Q        string   `short:"q" help:"full-text filter"`
	Max      int64    `help:"max events (0 = all)"`
}

func (c *ListCmd) Run(d *deps) error {
	ctx, cancel := ctxTimeout()
	defer cancel()
	app, err := newApp(ctx, c.Account)
	if err != nil {
		return err
	}
	return d.emit(app.List(ctx, c.Calendar, c.From, c.To, c.Q, c.Max))
}

type GetCmd struct {
	EventID  string `arg:"" name:"event-id" help:"event id"`
	Account  string `help:"account name"`
	Calendar string `help:"calendar id (default primary)"`
	Raw      bool   `help:"emit the full Google event resource"`
}

func (c *GetCmd) Run(d *deps) error {
	ctx, cancel := ctxTimeout()
	defer cancel()
	app, err := newApp(ctx, c.Account)
	if err != nil {
		return err
	}
	return d.emit(app.Get(ctx, c.Calendar, c.EventID, c.Raw))
}

type CreateCmd struct {
	Account     string `help:"account name"`
	Calendar    string `help:"target calendar id (default primary)"`
	Title       string `help:"event title"`
	Start       string `help:"start time"`
	End         string `help:"end time"`
	Description string `help:"event description"`
	JSON        string `name:"json" help:"read a full event body from a file or - for stdin"`
	DryRun      bool   `help:"print the change without applying it"`
	Raw         bool   `help:"emit the full Google event resource"`
}

func (c *CreateCmd) Run(d *deps) error {
	r, closeFn, err := jsonReader(c.JSON)
	if err != nil {
		return err
	}
	defer closeFn()
	ctx, cancel := ctxTimeout()
	defer cancel()
	app, err := newApp(ctx, c.Account)
	if err != nil {
		return err
	}
	return d.emit(app.Create(ctx, gcal.CreateParams{
		Calendar: c.Calendar, Title: c.Title, Start: c.Start, End: c.End,
		Description: c.Description, JSON: r, DryRun: c.DryRun, Raw: c.Raw,
	}))
}

type UpdateCmd struct {
	EventID     string `arg:"" name:"event-id" help:"event id"`
	Account     string `help:"account name"`
	Calendar    string `help:"calendar id (default primary)"`
	Title       string `help:"event title"`
	Start       string `help:"start time"`
	End         string `help:"end time"`
	Description string `help:"event description"`
	JSON        string `name:"json" help:"read a partial event body from a file or - for stdin"`
	Scope       string `default:"instance" help:"recurring scope: instance, following, or all"`
	DryRun      bool   `help:"print the change without applying it"`
	Raw         bool   `help:"emit the full Google event resource"`
}

func (c *UpdateCmd) Run(d *deps) error {
	r, closeFn, err := jsonReader(c.JSON)
	if err != nil {
		return err
	}
	defer closeFn()
	ctx, cancel := ctxTimeout()
	defer cancel()
	app, err := newApp(ctx, c.Account)
	if err != nil {
		return err
	}
	return d.emit(app.Update(ctx, gcal.UpdateParams{
		Calendar: c.Calendar, EventID: c.EventID, Title: c.Title, Start: c.Start, End: c.End,
		Description: c.Description, JSON: r, Scope: c.Scope, DryRun: c.DryRun, Raw: c.Raw,
	}))
}

type DeleteCmd struct {
	EventID  string `arg:"" name:"event-id" help:"event id"`
	Account  string `help:"account name"`
	Calendar string `help:"calendar id (default primary)"`
	Scope    string `default:"instance" help:"recurring scope: instance, following, or all"`
	Confirm  bool   `help:"confirm the deletion"`
	DryRun   bool   `help:"print the change without applying it"`
	Raw      bool   `help:"emit the full Google event resource"`
}

func (c *DeleteCmd) Run(d *deps) error {
	ctx, cancel := ctxTimeout()
	defer cancel()
	app, err := newApp(ctx, c.Account)
	if err != nil {
		return err
	}
	return d.emit(app.Delete(ctx, gcal.DeleteParams{
		Calendar: c.Calendar, EventID: c.EventID, Scope: c.Scope, Confirm: c.Confirm, DryRun: c.DryRun, Raw: c.Raw,
	}))
}

type QuickAddCmd struct {
	Text     []string `arg:"" optional:"" help:"natural-language event text"`
	Account  string   `help:"account name"`
	Calendar string   `help:"target calendar id (default primary)"`
	DryRun   bool     `help:"print the change without applying it"`
	Raw      bool     `help:"emit the full Google event resource"`
}

func (c *QuickAddCmd) Run(d *deps) error {
	ctx, cancel := ctxTimeout()
	defer cancel()
	app, err := newApp(ctx, c.Account)
	if err != nil {
		return err
	}
	return d.emit(app.QuickAdd(ctx, c.Calendar, strings.Join(c.Text, " "), c.DryRun, c.Raw))
}

type FreeBusyCmd struct {
	Account  string   `help:"account name"`
	Calendar []string `help:"calendar id (repeatable; default primary)"`
	From     string   `default:"now" help:"window start"`
	To       string   `default:"in 7 days" help:"window end"`
}

func (c *FreeBusyCmd) Run(d *deps) error {
	ctx, cancel := ctxTimeout()
	defer cancel()
	app, err := newApp(ctx, c.Account)
	if err != nil {
		return err
	}
	return d.emit(app.FreeBusy(ctx, c.Calendar, c.From, c.To))
}

type LogCmd struct {
	Account string `help:"account name"`
	Max     int    `help:"max ops (0 = all)"`
}

func (c *LogCmd) Run(d *deps) error {
	app, err := storeApp(c.Account)
	if err != nil {
		return err
	}
	return d.emit(app.Log(c.Max))
}

type RestoreCmd struct {
	OpID    string `arg:"" name:"op-id" optional:"" help:"op id to restore"`
	Account string `help:"account name"`
	Last    bool   `help:"restore the most recent op"`
	DryRun  bool   `help:"print the change without applying it"`
	Raw     bool   `help:"emit the full Google event resource"`
}

func (c *RestoreCmd) Run(d *deps) error {
	ctx, cancel := ctxTimeout()
	defer cancel()
	app, err := newApp(ctx, c.Account)
	if err != nil {
		return err
	}
	return d.emit(app.Restore(ctx, c.OpID, c.Last, c.DryRun, c.Raw))
}

type PruneCmd struct {
	Account string `help:"account name"`
	Before  string `help:"prune ops older than this time"`
	All     bool   `help:"prune all ops"`
}

func (c *PruneCmd) Run(d *deps) error {
	app, err := storeApp(c.Account)
	if err != nil {
		return err
	}
	return d.emit(app.Prune(c.Before, c.All))
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
