// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

// Package main is a Go library designed to enable command line interfaces
// that resemble popular network equipment.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gotermcli/routercli/auditlog"
	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/command"
	"github.com/gotermcli/routercli/completer"
	"github.com/gotermcli/routercli/config"
	"github.com/gotermcli/routercli/daemon"
	"github.com/gotermcli/routercli/example/cmd/core"
	"github.com/gotermcli/routercli/example/cmd/product"
	_ "github.com/gotermcli/routercli/example/cmd/session"
	"github.com/gotermcli/routercli/i18n"
	"github.com/gotermcli/routercli/paging"
	"github.com/gotermcli/routercli/tokenize"

	"github.com/chzyer/readline"
	"github.com/gologme/log"
	"github.com/pborman/getopt/v2"
	"golang.org/x/term"
)

// Version and Build - These global variables hold build information. The
// Build variable is populated by the Makefile and uses the Git Head hash
// as its identifier. Both variables are used in the console output for
// --version and --help.
var (
	Version = "0.1"
	Build   string
)

// errReported - This sentinel error means run has already printed its own
// diagnostic to stderr in a format of its own, the "%" prefixed style this
// project uses for configuration problems and for a refused login, and
// that main must therefore exit non-zero without printing anything
// further. Every other error run returns carries its own message and is
// printed by main.
var errReported = errors.New("routercli: startup failed, already reported")

// main - This function is kept to nothing but flag parsing, the call to
// run, and the process exit status. Every piece of real startup work lives
// in run, which returns an error rather than calling os.Exit itself. That
// split matters: os.Exit does not run deferred functions, so a startup
// failure inside a single large main would skip every defer registered
// before it, including remoteClient.Close, which is what sends the daemon
// a Goodbye message and lets it drop this session from "show users".
// Returning an error out of run instead lets every one of those defers run
// on the way out, no matter which step failed.
func main() {
	configFileName, configExplicit, checkConfig := processCommandLineFlags()
	if err := run(configFileName, configExplicit, checkConfig); err != nil {
		if !errors.Is(err, errReported) {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}

// run - This function performs every startup step this program needs, in
// order, and then hands control to runLoop for the life of the session. It
// returns nil on a clean exit, including the --check-config success path,
// and an error on any failure. See main's doc comment for why this returns
// an error rather than exiting directly.
func run(configFileName string, configExplicit, checkConfig bool) error {
	// --------------------------------------------------
	// Load System Configuration
	// --------------------------------------------------
	// A configuration file the operator named explicitly must exist and
	// must be valid; a typo in --config fails here rather than starting
	// the program on defaults nobody asked for. The built in default
	// path is allowed to be missing, so a deployment can run before it
	// has written a configuration file of its own.
	load := config.LoadSystemConfigOrDefaults
	if configExplicit {
		load = config.LoadSystemConfig
	}
	cfg, err := load(configFileName)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	logger, closeLog := newLogger(cfg, "")
	defer closeLog()

	// --------------------------------------------------
	// Setup translator for help files and descriptions
	// --------------------------------------------------
	catalogs, err := i18n.LoadCatalogs(cfg.LanguageDir)
	if err != nil {
		return fmt.Errorf("failed to load language catalogs: %w", err)
	}
	translator := i18n.New(catalogs, cfg.CurrentLanguage, cfg.DefaultLanguage)

	// --------------------------------------------------
	// Configure System
	// --------------------------------------------------
	if cfg.PreventEscape {
		preventEscape()
	}
	levels, roles, err := loadCommandLevels(cfg, logger)
	if err != nil {
		return err
	}
	if checkConfig {
		fmt.Println("configuration OK -", len(levels.Order), "Command Levels verified")
		return nil
	}

	// This local audit log is only opened when no daemon is configured.
	// Once one is, ctx.Audit is reassigned further down to the session's
	// RemoteClient, which sends each dispatched command to the daemon
	// rather than every CLI process appending to one file independently.
	//
	// It is constructed either way, never nil, so ctx.Audit always starts
	// from a real value and the deferred Close stays unconditional and
	// harmless even when nothing was ever opened.
	audit := auditlog.New(cfg.AuditLogFile, logger)
	if cfg.AuditLogEnabled && cfg.DaemonSocketPath == "" {
		if err := mkdirForFile(cfg.AuditLogFile); err != nil {
			return fmt.Errorf("failed to prepare audit log directory: %w", err)
		}
		if err := audit.Enable(); err != nil {
			return fmt.Errorf("failed to open audit log: %w", err)
		}
	}
	defer audit.Close()

	base := levels.Base()

	// A Session is always constructed, whatever AuthRequired says.
	// Elevation needs somewhere to track CommandLevel and
	// CommandLevelEnteredAt even when the login system is not in use.
	//
	// With AuthRequired off it stays Authenticated false with an empty
	// Username forever, which is the correct never logged in state rather
	// than a special case handlers must think about.
	//
	// CommandLevel is set here because auth.NewSession cannot: package auth
	// does not know what a Command Level is.
	session := auth.NewSession()
	session.CommandLevel = base.Name

	// users is loaded here, ahead of the AuthRequired block below, so the
	// daemon client's Store starts out sharing the exact same map ctx.Users
	// is about to be set to, rather than a disconnected copy.
	//
	// Several handlers call MutateUsers, and that closure must see the real
	// user database even when AuthRequired is false and no login ever runs.
	//
	// It stays nil exactly when AuthRequired is false, matching what
	// ctx.Users is left as.
	var users auth.Users
	if cfg.AuthRequired {
		loaded, err := auth.LoadUsers(cfg.UsersFile)
		if err != nil {
			return fmt.Errorf("failed to load users file: %w", err)
		}
		users = loaded
	}

	// productState and daemonClient exist as separate local variables,
	// rather than being built inline inside the AppContext literal below,
	// so daemonClient can be closed with defer right here, alongside every
	// other startup resource this function already closes that way, audit
	// above and histFile further down. Roles needs no equivalent hoist to
	// users above, since it is already loaded once, earlier in this
	// function, and shared by both daemonClient here and ctx.Roles below
	// with no separate AuthRequired branch of its own. See
	// command.DaemonClient's doc comment for the design this implements.
	productState := &product.State{}
	daemonClient := daemon.NewStandaloneClient(daemon.NewState(productState, levels, users, roles, nil))
	defer daemonClient.Close()

	ctx := newAppContext(cfg, levels, base, roles, session, translator, audit, productState, daemonClient, logger)

	// StartupConfigFile's directory is created unconditionally, the same
	// treatment histFile's directory gets further down, regardless of
	// whether a startup-config has ever been saved yet, since
	// StartupConfigFile always has a real default, see the field's
	// assignment right above. This is not strictly required for
	// command.LoadStartupConfig below, a missing file is not an error
	// either way, but it means the directory a first "write memory" needs
	// is already there from the very first run, rather than only appearing
	// the moment something is saved.
	if err := mkdirForFile(ctx.StartupConfigFile); err != nil {
		return fmt.Errorf("failed to prepare startup-config directory: %w", err)
	}

	// VendorConfigFile's directory gets the same unconditional treatment
	// as StartupConfigFile's above, and for the same reason:
	// VendorConfigFile always has a real default too, see the field's
	// assignment right above. writeVendorConfig
	// (example/cmd/product/cmd_startup_config.go) never creates the file itself
	// unless some level is VendorRestricted, so this is harmless, not
	// premature, for a deployment that never uses that feature at all.
	if err := mkdirForFile(ctx.VendorConfigFile); err != nil {
		return fmt.Errorf("failed to prepare vendor-config directory: %w", err)
	}

	// command.LoadStartupConfig runs before establishSession, and before
	// anything below constructs the real, interactive readline loop, so a
	// saved startup-config is applied to ctx.State the same way a real
	// device applies its own saved configuration before anyone can log in
	// at all, not gated behind who is about to connect. See
	// command.LoadStartupConfig's doc comment for the full reasoning,
	// including why this is safe to trust with no password prompting of
	// its own.
	if err := command.LoadStartupConfig(ctx, ctx.StartupConfigFile); err != nil {
		return fmt.Errorf("failed to load startup-config: %w", err)
	}

	// BannerMOTD, "message of the day," is shown unconditionally here, to
	// every connection, right after command.LoadStartupConfig above has
	// replayed any saved "banner motd" line back into ctx.State, and
	// before establishSession's login prompt, if any, runs below. This
	// matches real Cisco and HP, where a MOTD banner is about the
	// connection itself, shown regardless of whether a login prompt
	// follows at all. See example/cmd/product/cmd_banner.go and
	// product.State.BannerMOTD's doc comment.
	if state, ok := ctx.State.(*product.State); ok {
		printBanner(state.BannerMOTD)

		// state.Line, replayed the same way BannerMOTD just was, holds
		// this project's persistent "line" mode defaults, see
		// example/cmd/product/cmd_line.go and product.State.Line's doc comment.
		// Each field left nil here, "line length" for instance never
		// having been typed or saved, leaves the matching ctx field
		// exactly as config.DefaultPageLines, config.PagingEnabled, or
		// AppContext's zero value for DefaultTerminalWidth already set it
		// a few lines above, so an unconfigured "line" mode changes
		// nothing about this deployment's existing, config file driven
		// defaults.
		if state.Line.Length != nil {
			ctx.DefaultPageLines = *state.Line.Length
		}
		if state.Line.Width != nil {
			ctx.DefaultTerminalWidth = *state.Line.Width
		}
		if state.Line.Paging != nil {
			ctx.PagingEnabled = *state.Line.Paging
		}
	}

	if err := authenticateSession(cfg, ctx, users, base, translator, audit, logger); err != nil {
		return err
	}

	// A deployment with config.DaemonSocketPath left empty, the default,
	// leaves ctx.DaemonClient and ctx.Audit exactly as they already are,
	// so this project's entire standalone behavior is unchanged.
	// closeDaemon is safe to call either way.
	remoteClient, closeDaemon, err := connectDaemon(cfg, ctx, daemonClient)
	if err != nil {
		return err
	}
	defer closeDaemon()

	// The SESSION START entry is written unconditionally, after ctx.Session
	// has its final value, whatever AuthRequired says, so every connection
	// leaves a record of when it began rather than only ones that ran a
	// login prompt.
	//
	// ForceLog rather than Log, for the same reason ForceLog exists: a
	// session's start must never go missing just because something between
	// here and the end flipped audit logging off.
	//
	// It goes through ctx.Audit rather than the local log, so a session
	// connected to a daemon reports its start there like any other command.
	sessionStartText := "SESSION START"
	if ctx.Session.HostUsername != "" {
		sessionStartText = fmt.Sprintf("SESSION START (host account %q connected at %s)", ctx.Session.HostUsername, ctx.Session.HostConnectedAt.Format(time.RFC3339))
	}
	ctx.Audit.ForceLog(ctx.Session.Username, sessionStartText, true)

	// tty, not term: term is the imported golang.org/x/term package.
	tty, closeTerminal, err := startTerminal(cfg, ctx, logger)
	if err != nil {
		return err
	}
	defer closeTerminal()

	var farewellCh <-chan string
	if remoteClient != nil {
		farewellCh = remoteClient.FarewellChannel()
	}
	runLoop(tty.RL, tty.Listener, ctx, runLoopOptions{
		PreventEscape:      cfg.PreventEscape,
		SessionIdleTimeout: cfg.SessionIdleTimeout.AsDuration(),
		ElevationTimeout:   cfg.ElevationTimeout.AsDuration(),
		TerminalFD:         tty.FD,
		OrigTerminalState:  tty.OrigState,
		FarewellChannel:    farewellCh,
		RemoteClient:       remoteClient,
	})

	return nil
} // run()

// loadCommandLevels - This function turns the tree files on disk into a
// loaded, pruned, rate limited, and verified TreeStructure, plus the role
// set every allowed_roles reference is checked against.
//
// The order matters. Loading builds the structure. Pruning removes
// commands whose feature is off, so a disabled command never reaches help,
// completion, or verification at all. Rate limiter wiring is policy, kept
// out of loading. Verification runs last, against the finished structure.
//
// Every verification failure is reported in the "%" prefixed style and
// returns errReported, so the caller exits without printing a second
// message.
func loadCommandLevels(cfg config.SystemConfig, logger *log.Logger) (*command.TreeStructure, *command.RoleSet, error) {
	// The library ships no directory layout, so an unset TreeStructure means
	// no configuration file was found or the one found does not name a
	// manifest. Saying that plainly beats a missing file error pointing at a
	// path nobody chose.
	if cfg.TreeStructure == "" {
		return nil, nil, errors.New("no tree structure manifest is configured; pass --config naming a configuration file that sets TreeStructure")
	}

	levels, err := command.LoadTreeStructure(cfg.TreeStructure, cfg.CommonTreeFile)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load tree structure: %w", err)
	}
	// Every Command Level in the manifest is loaded and validated,
	// including its "run:" handler references, right here at startup. A
	// broken level_config_if.yaml fails the program immediately, not the
	// first time someone types "interface eth0" days from now. This covers
	// every level uniformly, root swap or nested. Nothing here names
	// "config" or "config-if" specifically. Adding a level to
	// tree_structure.yaml gets it loaded and validated the same way with
	// zero code changes, beyond the one small cmd_*.go file, in cmd/core
	// or cmd/product, every level's enter and exit command always needs.
	// See command/treestructure.go's top of file comment.

	// A command whose feature is off is pruned out of every level here,
	// before anything else touches these trees, so it never appears in
	// help, tab completion, or verification, rather than existing and
	// refusing.
	//
	// featureFlags is the complete set of names a tree file may reference
	// through requires. That naming convention is private to this file;
	// package command defines none of it. A new gated feature means a new
	// entry here naming whichever configuration boolean controls it.
	featureFlags := map[string]bool{
		"totp":            cfg.EnableTOTPAuthentication,
		"password_change": cfg.EnableCLIAuthentication,
		// "daemon" gates "show users" and "disconnect user", both
		// meaningless with no real daemon configured, an empty
		// DaemonSocketPath being the whole reason StandaloneClient's
		// ListUsers, DisconnectUser, Reboot, and FarewellChannel all
		// return ErrDaemonNotConfigured rather than doing something
		// halfway; see that error's doc comment in
		// command/daemonclient.go.
		"daemon": cfg.DaemonSocketPath != "",
	}
	// This runs before pruning on purpose. It asks whether the whole
	// configuration references every registered handler, not whether this
	// particular deployment's enabled features happen to reach them. A
	// command gated behind a feature flag that is off is pruned out of the
	// tree here, but its handler is still correctly registered for a
	// deployment that turns the feature on.
	if problems := command.VerifyRegisteredCommandsAreReachable(levels); len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "%", p)
		}
		return nil, nil, errReported
	}

	for _, level := range levels.Order {
		if err := command.PruneDisabledCommands(level.Tree, featureFlags, ""); err != nil {
			return nil, nil, fmt.Errorf("failed to prune disabled commands from the tree: %w", err)
		}
	}

	// Rate limiters are wired in here rather than during loading.
	// LoadTreeStructure builds a correct structure; how many attempts
	// before a lockout is policy, and does not belong in it.
	//
	// Every level gets one unconditionally, even with no PasswordHash set
	// right now, since "password manager" can set one at any time and the
	// limiter has to already be there when that happens.
	//
	// Per-command limiters are narrower: only commands with a password at
	// load time get one, since nothing sets a Command's password after the
	// tree is loaded.
	for _, level := range levels.Order {
		level.RateLimiter = auth.NewRateLimiter(cfg.CommandLevelMaxAttempts, cfg.CommandLevelAttemptWindow.AsDuration(), cfg.CommandLevelLockoutDuration.AsDuration())
	}
	attachPasswordRateLimiters(levels, cfg.CommandPasswordMaxAttempts, cfg.CommandPasswordAttemptWindow.AsDuration(), cfg.CommandPasswordLockoutDuration.AsDuration())

	// VerifyCommandLevels is a separate pass from loading, see its own doc
	// comment for why. It confirms every level's declared enter_command
	// and exit_command correspond to a real, registered command, meaning
	// someone did write the cmd_*.go file, in cmd/core or cmd/product, the
	// manifest expects, catching a typo or a forgotten file right here
	// instead of the first time a user types the command and gets "unknown
	// command". This runs unconditionally, not just under --check-config,
	// matching this project's convention that a broken configuration fails
	// loudly at startup.
	if problems := command.VerifyCommandLevels(levels); len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "%", p)
		}
		return nil, nil, errReported
	}
	if problems := command.VerifyVendorDefinedSecrets(levels); len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "%", p)
		}
		return nil, nil, errReported
	}

	// roles is a deployment's declared set of role names. A missing
	// RolesFile is not itself an error. VerifyRoles then checks every
	// allowed_roles reference across the whole tree against it, the same
	// "run this once at startup, fail loudly" convention
	// VerifyCommandLevels and VerifyVendorDefinedSecrets above already
	// follow, and for the same reason: an unknown role name should fail
	// the program immediately, not the first time a user hits the command
	// it silently makes unreachable.
	roles, err := command.LoadRoles(cfg.RolesFile)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load roles manifest: %w", err)
	}
	if problems := command.VerifyRoles(levels, roles); len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "%", p)
		}
		return nil, nil, errReported
	}
	warnPlaintextLevelSecrets(logger, levels)
	warnPlaintextCommandSecrets(logger, levels)

	return levels, roles, nil
}

// authenticateSession - This function performs every step
// config.AuthRequired turns on: it finishes populating the authentication
// related fields on ctx, builds the credential provider when CLI
// authentication is enabled, runs the login prompt, and forces a pending
// password change to completion before the session is allowed to continue.
// A deployment with AuthRequired off leaves ctx exactly as it was and
// returns nil, so the caller can always call this unconditionally. users
// is the already loaded map, the very same one daemonClient's Store was
// built from, rather than a second, freshly loaded copy, so ctx.Users and
// that Store can never diverge. base names the Command Level a newly
// established session starts at. A refused login and a password change
// that does not complete both print in this project's "%" prefixed style
// and return errReported, so the caller exits without printing a second,
// duplicate message.
func authenticateSession(cfg config.SystemConfig, ctx *command.AppContext, users map[string]*auth.User, base *command.CommandLevel, translator *i18n.Translator, audit *auditlog.AuditLog, logger *log.Logger) error {
	if !cfg.AuthRequired {
		return nil
	}

	// users itself was already loaded above, ahead of daemonClient's
	// construction; ctx.Users is set to that exact same map here, rather
	// than a second, freshly loaded one, so ctx.Users and daemonClient's
	// Store never diverge.
	ctx.Users = users
	ctx.UsersFile = cfg.UsersFile
	ctx.TOTPIssuer = cfg.TOTPIssuer
	ctx.TOTPMaxAttempts = cfg.TOTPMaxAttempts
	ctx.PasswordPolicy = auth.PasswordPolicy{
		MinLength:           cfg.PasswordMinLength,
		RequireUppercase:    cfg.PasswordRequireUppercase,
		RequireNumbers:      cfg.PasswordRequireNumbers,
		RequireSpecialChars: cfg.PasswordRequireSpecialChars,
	}
	ctx.PasswordChangeMaxAttempts = cfg.PasswordChangeMaxAttempts
	warnPlaintextUserSecrets(logger, users)

	// ctx.AuthProvider is only built when EnableCLIAuthentication is on,
	// since it is only ever consulted by auth.PromptLogin's credential
	// check, inside establishSession below, and by
	// example/cmd/core/cmd_password.go's password change re-authentication step.
	// That password change command itself sets requires: password_change,
	// so it is pruned out of the tree entirely whenever
	// EnableCLIAuthentication is off, see the featureFlags pruning pass
	// above. Leaving this nil in the host-only case is therefore safe:
	// nothing reachable in that configuration ever calls
	// ctx.AuthProvider.Authenticate.
	if cfg.EnableCLIAuthentication {
		provider, err := auth.NewProvider(cfg.CLIAuthProvider, users)
		if err != nil {
			return fmt.Errorf("failed to construct authentication provider: %w", err)
		}
		ctx.AuthProvider = provider
	}

	// BannerLogin is shown immediately before this call only when
	// EnableCLIAuthentication is on, since that is the one condition under
	// which establishSession below runs a real, interactive username
	// prompt at all, auth.PromptLogin inside establishSession. A
	// deployment running EnableHostAuthentication alone never shows a
	// login prompt, so a login banner would have nothing to introduce.
	if cfg.EnableCLIAuthentication {
		if state, ok := ctx.State.(*product.State); ok {
			printBanner(state.BannerLogin)
		}
	}

	session, err := establishSession(cfg, users, ctx.AuthProvider, translator, audit, os.Stdin, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "\n%", translator.T("auth.access_denied"))
		return errReported
	}
	session.CommandLevel = base.Name
	ctx.Session = session

	// An account created with "account create" is forced straight into
	// changing its password here, before anything else runs, whenever its
	// MustChangePassword flag is set.
	//
	// This only applies when a real CLI login ran above. A session resolved
	// purely through host authentication has no users.yaml password to
	// force a change on.
	//
	// A change that does not complete refuses the session outright rather
	// than continuing with an account still carrying a placeholder
	// credential.
	if cfg.EnableCLIAuthentication {
		if u := ctx.Users[ctx.Session.Username]; u != nil && u.MustChangePassword {
			fmt.Println(translator.T("auth.must_change_password"))
			if err := core.RunPasswordChange(ctx, int(os.Stdin.Fd()), os.Stdout); err != nil {
				return fmt.Errorf("failed to change password: %w", err)
			}
			if u := ctx.Users[ctx.Session.Username]; u != nil && u.MustChangePassword {
				fmt.Fprintln(os.Stderr, "%", translator.T("auth.must_change_password_failed"))
				return errReported
			}
		}
	}

	return nil
}

// connectDaemon - A session that is going to share state with other
// sessions has to reach the daemon, and it must do so as an already
// identified user so its Hello carries a real username.
//
// This dials the daemon named by DaemonSocketPath and wires the connection
// onto ctx. It returns the client and a function the caller defers to
// close it. With DaemonSocketPath empty, the default, nothing is dialed,
// ctx is untouched, and the returned function does nothing, so a caller
// can defer it unconditionally.
//
// It MUST run after ctx.Session has its final value.
//
// ctx.DaemonClient becomes a ConnectedClient: session and connection
// tracking go across the socket, while product state, levels, users, and
// roles are still backed by the standalone client and still live only in
// this process. That is a disclosed limitation.
//
// ctx.Audit is repointed at the same connection, so every audit call
// already scattered through runLoop sends the daemon a message instead of
// writing a local file, with no change at those call sites.
//
// The terminal name reported by "show users" has no portable source in Go.
// SSH_TTY is used when set, since that is how most sessions arrive, and a
// plain "-" otherwise, the same placeholder real gear falls back to.
func connectDaemon(cfg config.SystemConfig, ctx *command.AppContext, daemonClient *daemon.StandaloneClient) (*daemon.RemoteClient, func(), error) {
	if cfg.DaemonSocketPath == "" {
		return nil, func() {}, nil
	}

	publicKeyPath := daemon.StaticKeyPath(cfg.DaemonSocketPath) + ".pub"
	daemonPublicKey, err := daemon.ReadStaticPublicKey(publicKeyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read the routerclid's static public key: %w", err)
	}

	ch, conn, err := daemon.Dial(cfg.DaemonSocketPath, daemonPublicKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to routerclid: %w", err)
	}

	terminal := os.Getenv("SSH_TTY")
	if terminal == "" {
		terminal = "-"
	}

	remoteClient, err := daemon.NewRemoteClient(ch, conn, ctx.Session.Username, terminal)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to establish a session with routerclid: %w", err)
	}

	ctx.DaemonClient = daemon.NewConnectedClient(daemonClient, remoteClient)
	ctx.Audit = remoteClient
	remoteClient.SetLevel(ctx.Session.CommandLevel)

	// config.AuditLogEnabled governs remoteClient's Enable exactly the
	// same way it already governs the local audit variable's Enable in
	// run, standalone mode's untouched behavior. This can never itself
	// fail, having no file of its own to open. "audit-log enable" and
	// "audit-log disable", in example/cmd/core/cmd_audit.go, still work exactly as
	// before against this same ctx.Audit afterward, toggling this at any
	// point during the session; only this one, startup time default
	// changes with a daemon configured.
	if cfg.AuditLogEnabled {
		_ = remoteClient.Enable()
	}

	return remoteClient, func() { remoteClient.Close() }, nil
}

// reportResolveProblem - This function prints the right diagnostic for a
// command line that did not resolve to something runnable, audits the
// attempt where that is appropriate, and reports true. It reports false
// when res names a real, runnable command, in which case the caller goes
// on to dispatch it. The distinctions here are deliberate and match real
// Cisco and HP. An ambiguous line lists the actual candidates as bare
// names, space separated, with no "%" wrapper, mirroring what the
// completer's Tab list prints for the same situation rather than inventing
// a separately worded message. "invalid input" names the first token that
// did not match, whether nothing matched at all or a real command was
// found and what followed it was not one of its children. "incomplete
// command" is reserved for the case where the user stopped short, a bare
// "show", with nothing left over to be wrong.
func reportResolveProblem(ctx *command.AppContext, res command.ResolveResult, line, username string) bool {
	switch {
	case len(res.Ambiguous) > 0:
		for _, candidate := range res.Ambiguous {
			fmt.Println(" " + candidate)
		}
		return true
	case res.Command == nil:
		// Nothing at the top level matched at all. res.Args holds the
		// unmatched token onward, see command.Resolve. The first one is
		// the actual offending word; anything after it was never even
		// looked at, so it is not part of the error.
		fmt.Println("%", ctx.Translator.T("runloop.invalid_input", firstBadToken(res.Args)))
		ctx.Audit.Log(username, line, false)
		return true
	case res.Command.RunFunc == nil && len(res.Args) > 0:
		// A real command was found, "show" for instance, but what followed
		// it does not match any of its children either, "show fan" where
		// "fan" is not a show subcommand.
		fmt.Println("%", ctx.Translator.T("runloop.invalid_input", firstBadToken(res.Args)))
		ctx.Audit.Log(username, line, false)
		return true
	case res.Command.RunFunc == nil:
		// A real container command, "show" on its own, or "configure"
		// without "terminal", with nothing left over. The user needs to
		// keep typing, not correct a typo.
		fmt.Println("%", ctx.Translator.T("runloop.incomplete_command", line))
		return true
	case res.Negated && !res.Command.Negatable:
		fmt.Println("%", ctx.Translator.T("runloop.not_negatable", strings.Join(res.FullName, " ")))
		ctx.Audit.Log(username, line, false)
		return true
	}
	return false
}

// newAppContext - This function assembles the single *command.AppContext
// every handler in every command package receives. It is pure
// construction: every value it needs is already resolved by the time it is
// called, and it performs no I/O and no validation of its own. The
// authentication related fields are left at their zero values here and
// filled in later by authenticateSession, when and only when
// config.AuthRequired is on. See that function for why.
func newAppContext(cfg config.SystemConfig, levels *command.TreeStructure, base *command.CommandLevel, roles *command.RoleSet, session *auth.Session, translator *i18n.Translator, audit *auditlog.AuditLog, productState *product.State, daemonClient *daemon.StandaloneClient, logger *log.Logger) *command.AppContext {
	return &command.AppContext{
		State:        productState,
		DaemonClient: daemonClient,
		Logger:       logger,
		Levels:       levels,
		Roles:        roles,
		Audit:        audit,
		Translator:   translator,
		// Seeded from the base level's Name, PromptSuffix, and Tree, never
		// a hardcoded literal, so anything comparing against the current
		// root Command Level's name always matches whatever
		// tree_structure.yaml names the base level.
		Position: command.NewCommandLevelStack(base.Name, base.PromptSuffix, base.Tree),
		Session:  session,
		// Built once from config, here, and reused everywhere a listing of
		// more than one command name gets ordered: this
		// AppContext.ListOptions, the completer.New call below, and the
		// non-interactive "?" fallback further down runLoop, so all three
		// always agree. See command.ListOptions's doc comment.
		ListOptions: command.ListOptions{
			Alphabetical: cfg.AlphabeticalCommandOrder,
			MergeCommon:  cfg.MergeCommonCommands,
		},
		// PageLines starts nil, unset, matching real Cisco's terminal
		// length, session only, never seeded from a configuration file.
		// DefaultPageLines, PagingEnabled, and FilterMode's startup value
		// all come from config here, the one place that already knows
		// them, rather than reread from config a second time anywhere
		// paging.EffectivePageLines or paging.Display are called.
		// filterModeFromConfig converts config.FilterMatchMode's plain
		// YAML string into package paging's typed FilterMode here, in
		// main.go, keeping package config free of any dependency on
		// package paging, the same boundary this project already keeps
		// between package config and package command or auth.
		DefaultPageLines:    cfg.DefaultPageLines,
		PagingEnabled:       cfg.PagingEnabled,
		FilterMode:          filterModeFromConfig(cfg.FilterMatchMode),
		MaxFilterChainDepth: cfg.MaxFilterChainDepth,
		// DefaultHistorySize is copied straight from config here as well,
		// the same treatment as DefaultPageLines just above. HistorySize
		// itself starts nil, unset, matching PageLines and TerminalWidth,
		// until "terminal history size" is typed. HistoryFile is set
		// further down, once histFile below has resolved
		// config.HistoryFile's possibly empty value to its real, final
		// path.
		DefaultHistorySize: cfg.DefaultHistorySize,
		// ReauthGracePeriod and AuthBypassTrustWindow are copied straight
		// from config here as well, the same as DefaultPageLines and the
		// rest above, so command.EnterCommandLevel can read them directly
		// off ctx rather than package command needing any dependency on
		// package config. See CommandLevel.LastAuthenticatedAt,
		// CommandLevel.GrantAuthBypass, and AppContext.ReauthGracePeriod /
		// AuthBypassTrustWindow's doc comments in command/model.go for what
		// each controls.
		ReauthGracePeriod:     cfg.ReauthGracePeriod.AsDuration(),
		AuthBypassTrustWindow: cfg.AuthBypassTrustWindow.AsDuration(),
		// StartupConfigFile is threaded through unconditionally, unlike
		// UsersFile below, since it has a real default regardless of
		// AuthRequired, see config.DefaultSystemConfig. ProductName is
		// threaded through the same way, for the same reason: "help
		// <command>" builds its own man page style header from it whether
		// or not this deployment requires authentication at all.
		StartupConfigFile: cfg.StartupConfigFile,
		VendorConfigFile:  cfg.VendorConfigFile,
		ProductName:       cfg.ProductName,
		RolesFile:         cfg.RolesFile,
		DefaultsDir:       cfg.DefaultsDir,
		// AuthRequired is copied straight from config here as well, the
		// same treatment RolesFile just above gets, so command.Authorized
		// can tell whether this deployment has authentication turned on at
		// all. See command.AppContext.AuthRequired's doc comment.
		AuthRequired: cfg.AuthRequired,
		// ReloadScheduler backs "reload <seconds>" and "reboot <seconds>",
		// see example/cmd/core/cmd_admin.go and
		// command.AppContext.ReloadScheduler's doc comment. Constructed
		// once, here, for the life of this connection.
		ReloadScheduler: command.NewPendingReload(),
	}
}

// passesPreExecutionGates - Several checks can refuse a resolved command,
// and each has to happen before the handler runs. Doing them inline in the
// loop buried what was a gate and what was dispatch.
//
// This runs them all and reports whether the command may run. It reports
// false only after printing the reason and auditing the refusal, so the
// caller just moves to the next line.
//
// The order runs cheapest first:
//
// A filter typed against a command that is not Pageable is caught first.
// There is nothing to capture or page, so that is a real error rather than
// a silent no-op.
//
// The role check is next. It is independent of any password, and a command
// can carry either, both, or neither.
//
// The password gate runs before ValidateArgs so a gated command does not
// leak its argument shape to a session that has not supplied the password.
// Its rate limiter is checked before prompting, since a locked out session
// should not be invited to try again. This password is asked for every
// time, unlike a Command Level password, because it gates one action
// rather than a mode a session settles into.
//
// ValidateArgs is skipped for a negated command, since "no X" often takes
// a different argument shape than "X". A negatable handler checks len(args)
// itself if it cares.
func passesPreExecutionGates(ctx *command.AppContext, res command.ResolveResult, stages []paging.FilterStage, line, username string) bool {
	// Checked before anything else in this branch. A filter was typed,
	// len(stages) > 0, but this command was never marked Pageable, see
	// command.Command.Pageable's doc comment, so there is nothing here to
	// capture, filter, or page. This is a real error, not a silent no-op,
	// the same "fail loudly" convention this project already applies to
	// every other malformed request.
	if len(stages) > 0 && !res.Command.Pageable {
		fmt.Println("%", ctx.Translator.T("runloop.not_pageable", strings.Join(res.FullName, " ")))
		ctx.Audit.Log(username, line, false)
		return false
	}
	// Checked next, before the password gate below, the same "cheapest,
	// non-interactive check first" ordering command.EnterCommandLevel now
	// follows for a Command Level's AllowedRoles. See
	// Command.AllowedRoles's doc comment: this is independent of
	// EffectivePasswordHash below, a command can carry either, both, or
	// neither, and both are enforced when both are set.
	if len(res.Command.AllowedRoles) > 0 && !command.Authorized(ctx, res.Command.AllowedRoles) {
		fmt.Println("%", ctx.Translator.T("commandlevel.access_denied"))
		ctx.Audit.Log(username, line, false)
		return false
	}
	// Checked next, before ValidateArgs. See this function's doc comment
	// for why. It avoids leaking a password- gated command's argument
	// shape requirements to a session that has not supplied the password
	// yet. This is independent of Command Level entirely. A command can
	// carry its own PasswordHash regardless of which level the session is
	// currently in. It is reprompted on every invocation, not cached for
	// the session the way a level's PasswordHash is, since this is a
	// one-off gate on this one action, not something a session settles
	// into. See Command.PasswordHash's doc comment for how this differs
	// from CommandLevel.PasswordHash.
	if effectiveHash := res.Command.EffectivePasswordHash(); effectiveHash != "" {
		// Checked before ever prompting, the same reasoning as
		// command.EnterCommandLevel's rate limit check: a locked out
		// session should not be invited to try again at all.
		if ok, retryAfter := res.Command.PasswordRateLimiter.Allow(); !ok {
			fmt.Println("%", ctx.Translator.T("auth.too_many_attempts", auth.RoundForDisplay(retryAfter)))
			ctx.Audit.Log(username, line, false)
			return false
		}
		password, perr := auth.PromptSecret(os.Stdout, int(os.Stdin.Fd()), ctx.Translator)
		if perr != nil || !auth.VerifyPassword(effectiveHash, password) {
			res.Command.PasswordRateLimiter.RecordFailure()
			fmt.Println("%", ctx.Translator.T("commandlevel.access_denied"))
			ctx.Audit.Log(username, line, false)
			return false
		}
		res.Command.PasswordRateLimiter.RecordSuccess()
	}
	// ValidateArgs is skipped entirely for a negated command. See
	// command.ValidateArgs's doc comment for why "no X" often has a
	// different valid argument shape than "X" itself, for example "no
	// description" takes zero args to clear a value that "description
	// <text>" requires exactly one to set. A negatable handler is expected
	// to check len(args) itself if it cares.
	if !res.Negated {
		if err := command.ValidateArgs(res.Command, res.Args); err != nil {
			fmt.Printf("%% %v\n", err)
			ctx.Audit.Log(username, line, false)
			return false
		}
	}

	return true
}

// dispatchCommand - This function runs one already resolved and already
// permitted command, handles its audit entry, and refreshes the prompt
// afterward. It reports true when the session should end, which is what
// command.ErrQuit means, and false to carry on reading lines. Whether this
// invocation would be logged is snapshotted both before and after RunFunc,
// and the entry is written if either side was loggable. That is not
// redundancy: a command such as "audit-log disable" or "audit-log enable"
// changes the audit enabled state as its own side effect, so checking only
// afterward would mean "disable" could never log its own invocation, and
// checking only beforehand would mean "enable" could never log its own.
// Logging on either side makes both transitions visible, which is exactly
// the pair of events an audit log exists to capture. SetLevel runs before
// that audit logging rather than after, so a command that changes
// ctx.Session.CommandLevel, "enable" among them, reports its own new,
// already current level on the very AuditEvent this dispatch is about to
// send, rather than the level it ran from. A connected daemon's "show
// users" therefore always reflects a session's freshest Command Level,
// never one dispatch stale.
func dispatchCommand(rl *readline.Instance, treeListener *completer.TreeListener, ctx *command.AppContext, res command.ResolveResult, stages []paging.FilterStage, opts runLoopOptions, line, username string) bool {
	// Snapshot whether this would be logged before running the command,
	// not only after. A command such as "audit-log disable" or "audit-log
	// enable" changes the audit enabled state as its own side effect.
	// Checking only the state after RunFunc would mean "disable" could
	// never log its own invocation, since the state was just turned off,
	// and checking only the state before RunFunc would mean "enable" could
	// never log its own invocation, since the state had not turned on yet.
	// Logging if either side was loggable makes both transitions visible
	// in the trail, exactly the pair of events an audit log exists to
	// capture. See auditlog.AuditLog's doc comments for the ForceLog
	// reasoning.
	wasLoggable := ctx.Audit.WouldLog()
	// ctx.Negated is only meaningful for the duration of this one call.
	// See AppContext.Negated's doc comment. It is always reset after, so a
	// later non-negated command in the same session never accidentally
	// inherits it.
	ctx.Negated = res.Negated
	var runErr error
	if res.Command.Pageable {
		// See dispatchPageable's doc comment for the capture, filter, and
		// page sequence this runs instead of calling RunFunc directly. A
		// Pageable command is exactly the kind this is safe for, one whose
		// entire output is produced up front with no interactive prompt of
		// its own reading from the terminal partway through.
		runErr = dispatchPageable(ctx, res, stages, opts.TerminalFD)
	} else {
		runErr = res.Command.RunFunc(ctx, res.Args)
	}
	ctx.Negated = false
	// SetLevel runs before the audit logging just below, not after,
	// specifically so a command that itself changes
	// ctx.Session.CommandLevel, "enable" among them, reports its own new,
	// already-current level on the very AuditEvent this same dispatch is
	// about to send, rather than the level it ran from; a connected
	// daemon's "show users" therefore always reflects a session's freshest
	// Command Level as of its most recently dispatched command, never one
	// dispatch stale. See daemon.RemoteClient.SetLevel's doc comment.
	if opts.RemoteClient != nil {
		opts.RemoteClient.SetLevel(ctx.Session.CommandLevel)
	}
	isLoggableNow := ctx.Audit.WouldLog()
	if wasLoggable || isLoggableNow {
		ctx.Audit.ForceLog(username, line, runErr == nil)
	}
	if runErr == command.ErrQuit {
		logSessionEnd(ctx)
		return true
	}
	if runErr != nil {
		fmt.Printf("%% %v\n", runErr)
	}
	rl.SetPrompt(buildPrompt(ctx))
	treeListener.SetPrompt(buildPrompt(ctx))

	return false
}

// endSessionNow - This function ends the session immediately and does not
// return. It writes the SESSION END audit entry, restores the terminal if
// this session put it into raw mode, and exits the process. The three
// cases that reach it, an idle timeout, a scheduled reload firing, and a
// farewell from a connected daemon, all share one problem: the goroutine
// blocked on rl.Readline may still be waiting on real terminal input right
// now, with no portable way to cancel it. Returning command.ErrQuit
// through the ordinary dispatch path is therefore not available, so each
// of them tears down here directly instead. Because os.Exit does not run
// deferred functions, the terminal restore and the audit entry both have
// to happen here rather than being left to a defer in run.
func endSessionNow(ctx *command.AppContext, opts runLoopOptions) {
	logSessionEnd(ctx)
	if opts.OrigTerminalState != nil {
		_ = term.Restore(opts.TerminalFD, opts.OrigTerminalState)
	}
	os.Exit(0)
}

// readLine - This function reads one submitted line from the terminal,
// racing that blocking read against the three things that can end a
// session without the user ever pressing return: an idle timeout, a
// scheduled reload or reboot firing, and a farewell from a connected
// daemon. It returns the line and whatever error the reader reported,
// readline.ErrInterrupt and io.EOF among them, for the caller to
// interpret. If one of the other three cases wins the race instead, this
// does not return at all; see endSessionNow.
func readLine(rl *readline.Instance, ctx *command.AppContext, opts runLoopOptions) (string, error) {
	// The blocking read always runs in its own goroutine, not just when
	// opts.SessionIdleTimeout is set, so this select can also race it
	// against ctx.ReloadScheduler firing. A scheduled "reload <seconds>"
	// or "reboot <seconds>" can be armed at any point during an already
	// running session, unlike opts.SessionIdleTimeout, fixed for the whole
	// session before this loop ever starts, so there is no equivalent
	// "only bother select-ing when it matters" shortcut available here;
	// every iteration must be able to notice a fire, not only the ones
	// that happen to start after one was already scheduled.
	type readResult struct {
		line string
		err  error
	}
	resultCh := make(chan readResult, 1)
	go func() {
		l, e := rl.Readline()
		resultCh <- readResult{l, e}
	}()

	// idleCh is left nil, and therefore never selected, see the Go
	// language specification on a nil channel in a select statement,
	// whenever opts.SessionIdleTimeout is zero, preserving this project's
	// original "idle timeout off by default" behavior exactly.
	var idleCh <-chan time.Time
	if opts.SessionIdleTimeout > 0 {
		idleCh = time.After(opts.SessionIdleTimeout)
	}
	// reloadFireCh is left nil the same way, only when ctx.ReloadScheduler
	// itself is nil, the case in a hand built *command.AppContext, a test
	// for instance, that never constructed one. A real ctx wired up
	// through main above always sets this.
	var reloadFireCh <-chan struct{}
	if ctx.ReloadScheduler != nil {
		reloadFireCh = ctx.ReloadScheduler.FireChannel()
	}

	select {
	case res := <-resultCh:
		return res.line, res.err
	case <-idleCh:
		fmt.Println(ctx.Translator.T("runloop.idle_timeout"))
		endSessionNow(ctx, opts)
	case <-reloadFireCh:
		// A scheduled "reload <seconds>" or "reboot <seconds>" just fired,
		// uncancelled. This performs the exact same file re-reading work
		// an immediate "reload" does, see core.ReloadFromDiskForRestart,
		// then ends the connection the same direct way the idle timeout
		// branch just above does, rather than returning command.ErrQuit
		// through the ordinary dispatch path below: the goroutine reading
		// rl.Readline() above may still be blocked waiting on real
		// terminal input right now, with no portable way to cancel it, the
		// same reasoning opts.SessionIdleTimeout's doc comment gives in
		// full.
		fmt.Println(ctx.Translator.T("reboot.timer_fired"))
		if rerr := core.ReloadFromDiskForRestart(ctx); rerr != nil {
			fmt.Fprintf(os.Stderr, "%% %v\n", rerr)
		}
		endSessionNow(ctx, opts)
	case text := <-opts.FarewellChannel:
		// A connected daemon just ended this session on purpose,
		// "disconnect user" or "reboot" elsewhere having targeted it, or
		// this session's connection to the daemon was lost outright;
		// either way, text is already the full, human readable reason, see
		// command.DaemonClient.FarewellChannel's doc comment, printed here
		// exactly as received rather than wrapped in a further "%"
		// prefixed message of this project's. This ends the connection the
		// same direct way the idle timeout and reload fired branches just
		// above do, for the same reason: the goroutine reading
		// rl.Readline() above may still be blocked waiting on real
		// terminal input right now, with no portable way to cancel it.
		fmt.Println(text)
		endSessionNow(ctx, opts)
	}

	// Unreachable: every select case above either returns or ends the
	// process through endSessionNow, which does not return.
	return "", nil
}

// terminalSession - This type carries the four pieces of a live terminal
// that run has to hand to runLoop: the readline instance itself, the
// completer listening on it, and the file descriptor and saved state
// runLoop needs to restore the terminal on a path that cannot go through
// readline's Close. See endSessionNow for why that path exists.
type terminalSession struct {
	RL        *readline.Instance
	Listener  *completer.TreeListener
	FD        int
	OrigState *term.State
}

// startTerminal - This function resolves this session's history file,
// starts readline against it, attaches the tree completer, and begins
// watching for terminal resizes. It returns the assembled terminalSession
// and a function the caller defers to shut all of it down again.
func startTerminal(cfg config.SystemConfig, ctx *command.AppContext, logger *log.Logger) (*terminalSession, func(), error) {
	histFile := cfg.HistoryFile
	if histFile == "" {
		histFile = historyFilePath()
	}
	if err := mkdirForFile(histFile); err != nil {
		return nil, nil, fmt.Errorf("failed to prepare history file directory: %w", err)
	}
	// ctx.HistoryFile is set to this same, fully resolved path, so "show
	// history" in example/cmd/core/cmd_show.go reads the exact file readline
	// itself is about to open and append every submitted line to, rather
	// than rereading config.HistoryFile's possibly empty value and
	// reimplementing historyFilePath's fallback a second time.
	ctx.HistoryFile = histFile

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          buildPrompt(ctx),
		AutoComplete:    completer.NoopCompleter{},
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
		HistoryFile:     histFile,
		// Seeded from config here, once, at construction, so a deployment
		// that sets DefaultHistorySize away from readline's built-in 500
		// default gets that size from the very first line read. This is
		// never reassigned again later, unlike ctx's other session
		// overrides such as PageLines: readline.Config's HistoryLimit is
		// read, unsynchronized, by an internal background goroutine this
		// same NewEx call starts, github.com/chzyer/readline's
		// Operation.ioloop, for the entire life of this Instance, so
		// mutating it from outside after construction is a genuine data
		// race, not merely a theoretical one, and this project's "go test
		// -race" pass has caught it. "terminal history size <n>",
		// example/cmd/session/cmd_history.go, therefore governs only
		// command.EffectiveHistorySize's other consumer, "show history"'s
		// own display cap, see example/cmd/core/cmd_show.go's historyLines; it has
		// no live effect on this session's Up and Down arrow recall, which
		// stays fixed at whatever this one value was when this session
		// started.
		HistoryLimit: cfg.DefaultHistorySize,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start readline: %w", err)
	}

	treeListener := completer.New(ctx.Position, rl, logger, ctx.Translator, ctx.ListOptions)
	treeListener.SetPrompt(buildPrompt(ctx))
	treeListener.SetTerminalWidth(int(os.Stdin.Fd()), ctx.TerminalWidth, ctx.DefaultTerminalWidth)
	rl.Config.Listener = treeListener

	// The terminal state is captured independently of readline so the
	// paths that end a session without going through rl.Close can still
	// restore it; see endSessionNow. term.GetState fails harmlessly,
	// returning a nil state, when stdin is not a real terminal, such as
	// piped input or a test harness, which is fine since there is nothing
	// to restore in that case anyway.
	fd := int(os.Stdin.Fd())
	origState, _ := term.GetState(fd)

	stopResizeWatch := watchTerminalResize(ctx, fd)

	session := &terminalSession{RL: rl, Listener: treeListener, FD: fd, OrigState: origState}
	closeFn := func() {
		stopResizeWatch()
		rl.Close()
	}
	return session, closeFn, nil
}

// newLogger - This function builds this process's logger from cfg and
// returns it alongside a function the caller defers to release whatever
// the logger writes to. Output goes to cfg.LogFile when one is configured
// and to stderr otherwise. A LogFile that cannot be prepared or opened is
// a warning, not a fatal error: losing the log file is not a reason to
// refuse to start, so this falls back to stderr and says so. prefix is
// passed straight through to log.New, empty for the CLI and a name for the
// daemon. The returned close function is always safe to call, and does
// nothing when the logger is writing to stderr, so a caller can defer it
// unconditionally.
func newLogger(cfg config.SystemConfig, prefix string) (*log.Logger, func()) {
	output := io.Writer(os.Stderr)
	closeFn := func() {}

	if cfg.LogFile != "" {
		if err := mkdirForFile(cfg.LogFile); err != nil {
			fmt.Fprintln(os.Stderr, "warning: failed to prepare log file directory, falling back to stderr:", err)
		} else if f, err := os.OpenFile(cfg.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640); err != nil {
			fmt.Fprintln(os.Stderr, "warning: failed to open LogFile, falling back to stderr:", err)
		} else {
			output = f
			closeFn = func() { f.Close() }
		}
	}

	logger := log.New(output, prefix, log.LstdFlags)
	enableLogLevels(logger, cfg.LogLevel)
	return logger, closeFn
}

// enableLogLevels - This function turns on every logging level at or below
// verbosity. config.validate already restricts LogLevel to 0, 1, 3, or 5,
// so 0 enables nothing at all. The ROUTERCLI_DEBUG environment variable
// turns debug on by itself, for a one off debug run against a deployment
// whose configured level would not otherwise include it.
func enableLogLevels(logger *log.Logger, verbosity int) {
	if verbosity >= 1 {
		logger.EnableLevel("error")
		logger.EnableLevel("info")
	}
	if verbosity >= 3 {
		logger.EnableLevel("warn")
	}
	if verbosity >= 5 || os.Getenv("ROUTERCLI_DEBUG") != "" {
		logger.EnableLevel("debug")
	}
}

// establishSession - A deployment can authenticate by trusting the
// operating system account, by prompting for a CLI login, or by both. The
// session's identity has to be resolved the same way regardless.
//
// This resolves it from whichever combination is enabled and returns the
// session, or the first error. It runs once, only when AuthRequired is
// true, and configuration validation already guarantees at least one
// source is on.
//
// With host authentication alone, the session carries the operating system
// account and no password is asked for. If TOTP is also on and that
// account has a secret enrolled, a code is still required.
//
// With CLI authentication on, auth.PromptLogin drives the rest.
//
// With both on, the final Username is whoever the CLI login resolved to,
// while HostUsername still records which account the connection arrived
// as.
//
// A LOGIN audit entry is written for every attempt, success or failure.
//
// stdin and stdout are parameters rather than the process globals so a
// test can hand this a real pseudo terminal without mutating os.Stdin.
func establishSession(cfg config.SystemConfig, users auth.Users, provider auth.Provider, translator *i18n.Translator, audit *auditlog.AuditLog, stdin, stdout *os.File) (*auth.Session, error) {
	var hostSession *auth.Session
	if cfg.EnableHostAuthentication {
		s, err := auth.SessionFromHostIdentity()
		if err != nil {
			return nil, err
		}
		hostSession = s
	}

	if cfg.EnableCLIAuthentication {
		// A real *auth.KeyedRateLimiter only gets constructed when
		// LoginAttemptWindow is configured, meaning nonzero.
		// LoadSystemConfig's validate already guarantees that
		// LoginAttemptWindow and LoginLockoutDuration are either both zero
		// or both set, so checking just one here is sufficient. A nil rate
		// limiter tells PromptLogin to keep this project's original flat
		// maxAttempts behavior unchanged. See PromptLogin's doc comment.
		var loginRateLimiter *auth.KeyedRateLimiter
		if cfg.LoginAttemptWindow.AsDuration() > 0 {
			loginRateLimiter = auth.NewKeyedRateLimiter(cfg.LoginMaxAttempts, cfg.LoginAttemptWindow.AsDuration(), cfg.LoginLockoutDuration.AsDuration())
		}

		cliSession, err := auth.PromptLogin(stdin, stdout, int(stdin.Fd()), provider, users, cfg.EnableTOTPAuthentication, cfg.LoginMaxAttempts, loginRateLimiter, translator,
			func(username string) { audit.Log(username, "LOGIN", false) })
		if err != nil {
			return nil, err
		}
		if hostSession != nil {
			cliSession.HostUsername = hostSession.HostUsername
			cliSession.HostConnectedAt = hostSession.HostConnectedAt
		}
		audit.Log(cliSession.Username, "LOGIN", true)
		return cliSession, nil
	}

	// EnableCLIAuthentication is off, so hostSession is the whole story,
	// config.validateAuthSources already guarantees it is non-nil here.
	// The only thing left to decide is whether EnableTOTPAuthentication
	// requires a step up on top of it.
	if cfg.EnableTOTPAuthentication {
		u := users[hostSession.Username]
		if u == nil {
			// See auth.PromptLogin's doc comment for the same guard: a
			// resolved identity with no matching users.yaml entry has no
			// second factor to check.
			u = &auth.User{Username: hostSession.Username}
		}
		if auth.SecondFactorRequired(u) {
			reader := bufio.NewReader(stdin)
			verified := false
			for attempt := 1; attempt <= cfg.LoginMaxAttempts; attempt++ {
				if auth.VerifySecondFactor(stdout, reader, int(stdin.Fd()), u, translator) {
					verified = true
					break
				}
				audit.Log(hostSession.Username, "LOGIN", false)
			}
			if !verified {
				return nil, auth.ErrLoginFailed
			}
		}
	}
	audit.Log(hostSession.Username, "LOGIN", true)
	return hostSession, nil
}

// defaultHostnamePrompt - This constant is what the prompt shows before
// "hostname" has ever been set, or after "no hostname" resets it, matching
// product.defaultHostname (example/cmd/product/cmd_hostname.go), an unexported
// constant this file cannot reach directly. It is kept as a separate
// constant here rather than exporting that one because this is purely a
// display fallback, not the same value the framework treats as canonical.
const defaultHostnamePrompt = "router"

// buildPrompt - This function builds the current prompt string. For any
// privileged set of commands, it adds a "#" to the prompt. Privileged here
// means either the session is away from the base Command Level,
// !ctx.Session.AtLevel(ctx.Levels.Base().Name), or the Command Level stack
// is deeper than the root, meaning the session is inside a nested Command
// Level at all. The hostname portion reads
// ctx.State.(*product.State).Hostname live, so "hostname myrouter"
// immediately rewrites the prompt on the very next redraw. runLoop already
// calls this after every dispatch specifically so a command like this is
// reflected right away, not just remembered for next time. It falls back
// to defaultHostnamePrompt when Hostname is empty, which is exactly the
// state "no hostname" leaves it in, see example/cmd/product/cmd_hostname.go, so
// the fallback only ever shows up before a hostname has been set, or after
// it has been explicitly cleared, never as a silent default that masks a
// real configured value.
func buildPrompt(ctx *command.AppContext) string {
	frame := ctx.Position.Current()
	awayFromBase := false
	if ctx.Session != nil && ctx.Levels != nil {
		awayFromBase = !ctx.Session.AtLevel(ctx.Levels.Base().Name)
	}
	marker := "> "
	if awayFromBase || ctx.Position.Depth() > 1 {
		marker = "# "
	}

	host := defaultHostnamePrompt
	if state, ok := ctx.State.(*product.State); ok && state.Hostname != "" {
		host = state.Hostname
	}

	return host + frame.PromptSuffix + marker
}

// printBanner - This function prints text, either product.State.BannerMOTD
// or product.State.BannerLogin, example/cmd/product/model.go, to stdout as is, with
// no added formatting beyond its own trailing newline. It does nothing at
// all when text is empty, the default, unset state every banner starts in,
// and neither of its two call sites in main need their own separate empty
// check because of it. This is product package aware, the same established
// precedent buildPrompt already sets just above for reading Hostname back
// out of ctx.State, rather than adding a banner concept to the framework
// level AppContext itself; see BannerMOTD and BannerLogin's doc comments
// in example/cmd/product/model.go for why they live on ProductState instead.
func printBanner(text string) {
	if text == "" {
		return
	}
	fmt.Println(text)
}

// watchTerminalResize - This starts a goroutine that logs a debug entry
// each time the terminal behind fd is resized, by watching SIGWINCH. It
// returns a stop function, which the one caller defers so both the
// goroutine and the signal registration are cleaned up.
//
// This is observability only. Nothing here is needed for correctness,
// because nothing in RouterCLI caches a terminal size: both the page
// height and the width are read fresh on the call that needs them, so a
// resize is already picked up without being told about it.
func watchTerminalResize(ctx *command.AppContext, fd int) (stop func()) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-sigCh:
				w, h, err := term.GetSize(fd)
				if err != nil {
					continue
				}
				ctx.Logger.Debugln("DEBUG: terminal resized, now", w, "columns by", h, "lines")
			case <-done:
				return
			}
		}
	}()

	// sync.Once makes stop safe to call more than once. The caller wraps it
	// in a cleanup function it hands further up, and a cleanup function that
	// panics on a second call is a trap for whoever wires it in next.
	var once sync.Once
	return func() {
		once.Do(func() {
			signal.Stop(sigCh)
			close(done)
		})
	}
}

// runLoopOptions - This type carries the settings runLoop needs that do
// not belong on AppContext, because they describe one terminal session
// rather than the deployment.
//
// PreventEscape is the other half of preventEscape below. That function
// stops the operating system delivering SIGINT, SIGTSTP, SIGQUIT, and
// SIGTERM. Ctrl-C and Ctrl-D inside an active line read never reach it,
// because readline handles them as ordinary keystrokes, so closing that
// second door is this loop's job. When false, the default, Ctrl-C on an
// empty line exits and Ctrl-D exits. When true, neither does anything,
// and "exit" is the only way out.
//
// SessionIdleTimeout ends the session when no line arrives within that
// duration, matching Cisco's line exec-timeout. Readline has no read
// deadline, so the read runs in its own goroutine raced against a timer.
//
// When the timer wins, the abandoned goroutine stays blocked on the read
// forever, since a blocking terminal read cannot be cancelled portably.
// That is why the timeout path calls os.Exit rather than returning:
// rl.Close would deadlock trying to take a lock the abandoned Readline
// still holds. The terminal is restored directly through
// OrigTerminalState and TerminalFD instead, captured before readline put
// it into raw mode.
//
// ElevationTimeout demotes a session that has sat elevated with nothing
// typed. FarewellChannel and RemoteClient are nil unless a daemon is
// configured.
type runLoopOptions struct {
	PreventEscape      bool
	SessionIdleTimeout time.Duration
	ElevationTimeout   time.Duration
	// TerminalFD and OrigTerminalState are used only by the idle timeout
	// path, to restore the terminal without going through rl.Close(). See
	// this function's doc comment for why that distinction matters.
	// OrigTerminalState may be nil, for example when stdin is not a real
	// terminal, as in a test harness, and the restore is skipped in that
	// case, since there is nothing meaningful to restore.
	TerminalFD        int
	OrigTerminalState *term.State
	// FarewellChannel receives exactly one push, the human readable reason
	// text, the moment a connected daemon ends this session on purpose,
	// "disconnect user" or "reboot" elsewhere having targeted it, or the
	// connection to the daemon is lost with no explicit farewell at all.
	// Left nil whenever this deployment has no daemon configured, the same
	// nil channel convention idleCh and reloadFireCh below already
	// establish for "not wired up in this session," so the select below
	// never chooses this case.
	FarewellChannel <-chan string
	// RemoteClient, when non-nil, is this session's live connection to a
	// real daemon. The only thing runLoop itself uses it for is SetLevel,
	// called every time ctx.Session.CommandLevel changes, so a connected
	// daemon's "show users" always reports this session's current Command
	// Level rather than whatever it was the moment this session first
	// connected. Left nil whenever this deployment has no daemon
	// configured.
	RemoteClient *daemon.RemoteClient
}

// runLoop - This function is the actual read, resolve, validate, and
// dispatch loop, split out from main so it can be exercised without a real
// terminal attached. A command's PasswordHash, see Command.PasswordHash,
// is checked before ValidateArgs, so a session that does not know the
// password is refused before it ever learns whether its arguments would
// have been acceptable. This avoids leaking information about a
// password-gated command's argument shape to whoever just failed the
// password prompt. Resolution happens against ctx.Position.Current().Tree,
// not a fixed tree. This is what makes the whole mode system work,
// entering "configure terminal" pushes a new frame onto ctx.Position, and
// the very next loop iteration resolves against that frame's tree
// automatically, with no other change needed here. The prompt is rebuilt
// and pushed to readline with rl.SetPrompt() after every dispatch, since
// any command might have changed the mode or elevation state as a side
// effect. Checking whether the prompt changed first would save a few
// redraws, but is not worth the complexity given how infrequently commands
// change mode. Program termination is driven by command.ErrQuit, not by
// matching the name "exit". See example/cmd/core/cmd_mode_control.go's doc comment
// for why that matters now that "exit" behaves differently depending on
// mode depth.
func runLoop(rl *readline.Instance, treeListener *completer.TreeListener, ctx *command.AppContext, opts runLoopOptions) {
	for {
		line, err := readLine(rl, ctx, opts)

		if err == readline.ErrInterrupt {
			if !opts.PreventEscape && len(line) == 0 {
				break
			}
			continue
		} else if err == io.EOF {
			if opts.PreventEscape {
				fmt.Println(ctx.Translator.T("runloop.eof_blocked"))
				continue
			}
			break
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Checked here, after the blocking read returns rather than before
		// it starts, since the elapsed time that matters is how long it
		// has been since elevation as of right now, about to act on this
		// input, not as of when the wait for input began. Checking
		// beforehand would use a stale snapshot from before the possibly
		// long wait and demote one command too late.
		if opts.ElevationTimeout > 0 && ctx.Session != nil && ctx.Levels != nil && !ctx.Session.AtLevel(ctx.Levels.Base().Name) {
			if time.Since(ctx.Session.CommandLevelEnteredAt) > opts.ElevationTimeout {
				base := ctx.Levels.Base()
				ctx.Session.CommandLevel = base.Name
				ctx.Position.SetRootTree(base.Name, base.PromptSuffix, base.Tree)
				if opts.RemoteClient != nil {
					opts.RemoteClient.SetLevel(ctx.Session.CommandLevel)
				}
				fmt.Println(ctx.Translator.T("runloop.elevation_expired"))
				rl.SetPrompt(buildPrompt(ctx))
				treeListener.SetPrompt(buildPrompt(ctx))
			}
		}

		tokens, terr := tokenize.Tokenize(line)
		if terr != nil {
			fmt.Printf("%% %v\n", terr)
			continue
		}

		// cmdTokens is what gets resolved against the command tree.
		// segments is every "|..." stage that followed it, still raw and
		// unparsed at this point. A line with no "|" at all leaves
		// cmdTokens equal to tokens and segments nil, see
		// paging.SplitPipeline. stages is checked against
		// ctx.MaxFilterChainDepth right here, before resolution even runs,
		// so a line asking for too many filters is refused with one clear
		// error rather than silently truncated or evaluated anyway.
		cmdTokens, segments := paging.SplitPipeline(tokens)
		stages, perr := paging.ParseStages(segments, ctx.MaxFilterChainDepth)
		if perr != nil {
			fmt.Printf("%% %v\n", perr)
			continue
		}

		// Runtime defined command aliases, "alias <level> <name>
		// <word...>", see example/cmd/core/cmd_alias.go, are expanded here,
		// against cmdTokens[0] only, before either the "?" fallback below
		// or command.Resolve itself ever sees this line. This runs after
		// paging.SplitPipeline on purpose, so an alias can never itself
		// swallow a "| include" style filter segment typed after it; only
		// the command side of the line is ever checked against
		// command.CommandLevel.Aliases. See command.ExpandAlias's doc
		// comment for why this is a single, non-recursive pass.
		cmdTokens = command.ExpandAlias(ctx, cmdTokens)

		// Defensive, non-interactive "?" fallback. Normally,
		// readline.Listener's key == '?' branch, completer.go's
		// handleHelp, already intercepts '?' before it ever reaches here,
		// whether stdin is a real terminal or a plain pipe, since
		// handleHelp strips the '?' before the line is ever submitted.
		// This block is retained as defense for any other readline version
		// or environment where a non-tty stdin never triggers Listener
		// callbacks at all. It costs nothing to keep, and means a literal
		// trailing "?" token still gets a sensible answer instead of "%
		// Invalid input" if that assumption ever changes. Checked against
		// cmdTokens, never against a filter segment, since "?" only ever
		// means something as part of resolving a command, not as part of a
		// "| include" pattern.
		if len(cmdTokens) > 0 && cmdTokens[len(cmdTokens)-1] == "?" {
			width := paging.EffectiveTerminalWidth(opts.TerminalFD, ctx.TerminalWidth, ctx.DefaultTerminalWidth)
			help := command.HelpForPath(ctx.Position.Current().Tree, cmdTokens[:len(cmdTokens)-1], ctx.Translator, ctx.ListOptions, width)
			if help != "" {
				fmt.Print(help)
			}
			continue
		}

		res := command.Resolve(ctx.Position.Current().Tree, cmdTokens)
		username := ""
		if ctx.Session != nil {
			username = ctx.Session.Username
		}

		if reportResolveProblem(ctx, res, line, username) {
			continue
		}

		if !passesPreExecutionGates(ctx, res, stages, line, username) {
			continue
		}

		if dispatchCommand(rl, treeListener, ctx, res, stages, opts, line, username) {
			return
		}
	}
	// Reached by the two ordinary "break" exits above, Ctrl-C on an empty
	// line and Ctrl-D/EOF, neither of which goes through command.ErrQuit.
	// See this function's doc comment on opts.PreventEscape. The
	// command.ErrQuit path and the idle timeout path each log their own
	// SESSION END before leaving this function through return or os.Exit
	// respectively, since neither of those two ever reaches this line.
	logSessionEnd(ctx)
}

// logSessionEnd - This function writes the mandatory SESSION END audit
// entry, called from every real exit path out of runLoop, normal return,
// command.ErrQuit, and the idle timeout's os.Exit branch, so a session
// that started with a SESSION START entry, see main's logging right after
// establishSession returns, always gets a matching end entry too,
// regardless of which door it left through. ForceLog is used for the same
// reason main's SESSION START call uses it, see that call site's doc
// comment.
func logSessionEnd(ctx *command.AppContext) {
	username := ""
	if ctx.Session != nil {
		username = ctx.Session.Username
	}
	ctx.Audit.ForceLog(username, "SESSION END", true)
}

// runHashPasswordUtility - This function implements the --hashpassword
// flag, an administrator facing utility to generate a bcrypt hash for
// example/etc/users.yaml without needing to write a throwaway Go program to call
// auth.HashPassword directly. It reads the password once, with echo
// disabled, and prints the "$6$..." hash to stdout so it can be redirected
// or copied straight into the YAML file. stdin is taken as an explicit
// parameter rather than this function reading the process-wide os.Stdin
// itself, the same reasoning establishSession's doc comment gives, so a
// test can hand this the slave end of a real pseudo terminal instead of a
// real password prompt requiring an actual interactive session.
func runHashPasswordUtility(stdin *os.File) {
	fmt.Fprint(os.Stderr, "Password: ")
	pwBytes, err := term.ReadPassword(int(stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error reading password:", err)
		os.Exit(1)
	}
	hash, err := auth.HashPassword(string(pwBytes))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error hashing password:", err)
		os.Exit(1)
	}
	fmt.Println(hash)
}

// historyFilePath - This function returns a per-user history file path
// rather than a hardcoded one. It is used as the fallback when the
// configuration file does not set HistoryFile explicitly. It is not under
// logs/ by default, since a personal, shell style history file more
// naturally belongs in the user's home directory than in a
// project-relative logs folder that might not even be writable by them.
func historyFilePath() string {
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		return filepath.Join(u.HomeDir, ".routercli_history")
	}
	return ".routercli_history"
}

// mkdirForFile - This function ensures the directory containing path
// exists, so that a fresh checkout with an empty, or entirely missing,
// logs/ directory does not fail the first time something tries to write
// history or audit output there.
func mkdirForFile(path string) error {
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0750)
}

// attachPasswordRateLimiters - This function walks every loaded Command
// Level's tree and attaches a fresh auth.RateLimiter to each command whose
// EffectivePasswordHash, PasswordHash or VendorDefinedPasswordHash, is
// non-empty. See Command.PasswordRateLimiter's doc comment for what it is
// for. visited tracks *command.Command pointers, not names, since the same
// command can appear in more than one level's merged tree at once. A
// command inherited through InheritParent, see command.LoadTreeStructure,
// is the identical pointer in every level that inherits it, not a copy. A
// walk that did not track this would redo the work of attaching a limiter,
// and walking that command's subcommands, every time it re-encountered
// that shared command through a different level's tree, and would recurse
// forever on a hand-built, self-referencing tree. visited ensures exactly
// one limiter per unique command, regardless of how many levels can
// currently reach it.
func attachPasswordRateLimiters(levels *command.TreeStructure, maxAttempts int, window, lockout time.Duration) {
	visited := make(map[*command.Command]bool)
	var walk func(tree map[string]*command.Command)
	walk = func(tree map[string]*command.Command) {
		for _, cmd := range tree {
			if visited[cmd] {
				continue
			}
			visited[cmd] = true
			if cmd.EffectivePasswordHash() != "" {
				cmd.PasswordRateLimiter = auth.NewRateLimiter(maxAttempts, window, lockout)
			}
			walk(cmd.Subcommands)
		}
	}
	for _, level := range levels.Order {
		walk(level.Tree)
	}
}

// warnPlaintextUserSecrets - This function logs a warning for every user
// whose PasswordHash is stored in the plaintext "$0$..." form rather than a
// real hash.
//
// It is purely informational. It never refuses to start or blocks a login,
// because plaintext storage is a supported mode for the local development
// and testing it exists for.
//
// The warning exists so nobody ships that mode without noticing. It
// surfaces in the configured log, where an operator will see it, rather
// than only in a source comment they would have to know to go read.
func warnPlaintextUserSecrets(logger *log.Logger, users auth.Users) {
	for _, name := range sortedUserNames(users) {
		u := users[name]
		if auth.IsPlaintextHash(u.PasswordHash) {
			logger.Warnln("WARN: user", name, "has a plaintext (non-hashed) password set - this must never be used in a real deployment")
		}
	}
}

// sortedUserNames - This function returns every username in users, sorted,
// so warnPlaintextUserSecrets's output is stable between runs, since Go
// map iteration order is randomized, rather than shuffling which warning
// prints first every time the same broken users.yaml is loaded.
func sortedUserNames(users auth.Users) []string {
	names := make([]string, 0, len(users))
	for name := range users {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// warnPlaintextLevelSecrets - This function is the Tree Structure
// equivalent of warnPlaintextUserSecrets above. It warns, at "warn" level,
// about any Command Level whose PasswordHash or VendorDefinedPasswordHash
// is stored in plaintext form. This can only happen if
// example/var/tree/tree_structure.yaml itself was hand-edited with a raw "$0$..."
// value, since "password manager <secret>",
// example/cmd/core/cmd_password_manager.go, always calls auth.HashPassword, which
// never produces the plaintext form, and nothing in this project ever
// writes VendorDefinedPasswordHash at all, that field only ever comes from
// a hand-authored manifest. So this path only catches a manifest that was
// authored that way directly, the same testing convenience reasoning as
// the login side, and it matters more for a plaintext
// VendorDefinedPasswordHash than an ordinary one, since that field is the
// one meant to stay secret from the end user reading their own
// configuration.
func warnPlaintextLevelSecrets(logger *log.Logger, levels *command.TreeStructure) {
	for _, level := range levels.Order {
		if auth.IsPlaintextHash(level.PasswordHash) {
			logger.Warnln("WARN: Command Level", level.Name, "has a plaintext (non-hashed) password_hash set - this must never be used in a real deployment")
		}
		if auth.IsPlaintextHash(level.VendorDefinedPasswordHash) {
			logger.Warnln("WARN: Command Level", level.Name, "has a plaintext (non-hashed) vendor_defined_password_hash set - this must never be used in a real deployment")
		}
	}
}

// warnPlaintextCommandSecrets - This function is
// warnPlaintextLevelSecrets's counterpart for an individual Command's
// PasswordHash and VendorDefinedPasswordHash, walking every loaded Command
// Level's tree the same visited-pointer way attachPasswordRateLimiters
// does, so a command reachable through more than one level, by
// InheritParent, is still only warned about once.
func warnPlaintextCommandSecrets(logger *log.Logger, levels *command.TreeStructure) {
	visited := make(map[*command.Command]bool)
	var walk func(path string, tree map[string]*command.Command)
	walk = func(path string, tree map[string]*command.Command) {
		for name, cmd := range tree {
			if visited[cmd] {
				continue
			}
			visited[cmd] = true
			full := name
			if path != "" {
				full = path + " " + name
			}
			if auth.IsPlaintextHash(cmd.PasswordHash) {
				logger.Warnln("WARN: command", full, "has a plaintext (non-hashed) password_hash set - this must never be used in a real deployment")
			}
			if auth.IsPlaintextHash(cmd.VendorDefinedPasswordHash) {
				logger.Warnln("WARN: command", full, "has a plaintext (non-hashed) vendor_defined_password_hash set - this must never be used in a real deployment")
			}
			walk(full, cmd.Subcommands)
		}
	}
	for _, level := range levels.Order {
		walk("", level.Tree)
	}
}

// dispatchPageable - This function runs a Pageable command's RunFunc
// through paging.CaptureOutput, then paging.ApplyFilters, then
// paging.Display, in that order, in place of calling RunFunc directly the
// way runLoop's default branch does for every other command. See
// Command.Pageable's doc comment for which commands this is safe for at
// all. A failure inside paging.CaptureOutput itself, essentially
// impossible in practice, is returned directly, since nothing was captured
// at all and there is nothing left to filter or page. A failure inside
// paging.ApplyFilters, an invalid regular expression typed as a filter
// pattern for instance, only possible when ctx.FilterMode is
// FilterModeRegex, is printed directly here rather than returned, since it
// describes the filter itself, not the command that produced the output
// being filtered; RunFunc's runErr is still returned afterward either way,
// so a command that both printed something and returned an error still
// reports that error through this function's caller exactly as it would
// have running unpaged.
func dispatchPageable(ctx *command.AppContext, res command.ResolveResult, stages []paging.FilterStage, fd int) error {
	var runErr error
	lines, cerr := paging.CaptureOutput(func() { runErr = res.Command.RunFunc(ctx, res.Args) })
	if cerr != nil {
		return cerr
	}

	filtered, ferr := paging.ApplyFilters(lines, stages, ctx.FilterMode)
	if ferr != nil {
		fmt.Printf("%% %v\n", ferr)
		return runErr
	}

	pageLines := paging.EffectivePageLines(fd, ctx.PageLines, ctx.DefaultPageLines)
	if derr := paging.Display(os.Stdout, fd, ctx.Translator, filtered, pageLines, ctx.PagingEnabled); derr != nil {
		fmt.Printf("%% %v\n", derr)
	}
	return runErr
}

// filterModeFromConfig - This function converts
// config.SystemConfig.FilterMatchMode's plain YAML string into package
// paging's typed FilterMode, the one place this project crosses that
// boundary, so package config never needs to import package paging just to
// describe its own default. config.LoadSystemConfig's validate already
// rejects any value other than "substring" or "regex" before this ever
// runs, so the switch below has no error path of its own; an unrecognized
// value cannot reach here from a real, loaded configuration.
func filterModeFromConfig(mode string) paging.FilterMode {
	if mode == "regex" {
		return paging.FilterModeRegex
	}
	return paging.FilterModeSubstring
}

// firstBadToken - This function returns the first entry of a
// ResolveResult's Args slice, or an empty string if it is empty. It is
// extracted so the runLoop switch above does not repeat the same guard
// twice. Both res.Command == nil and res.Command.RunFunc == nil with
// leftover Args need the one bad word, not the whole leftover tail. "show
// fan extra junk" should report "Invalid input: fan", not "Invalid input:
// fan extra junk", matching how a real device points at the first word it
// could not place and stops there without trying to interpret what follows
// it.
func firstBadToken(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// -------------------------------------------------- Private functions
// --------------------------------------------------

// preventEscape - This function catches various signals to prevent a user
// from escaping the CLI into a standard command shell through things like
// Ctrl-C, Ctrl-Z suspend, Ctrl-\, or a plain kill on the process. The only
// sanctioned way out then becomes the "exit" command itself. This is an
// optional configuration setting, and should only be used in production
// systems. Deliberately not covered here are SIGABRT, SIGSEGV, and SIGILL,
// since these mean something to the Go runtime itself, which uses SIGSEGV
// and SIGILL for parts of its own crash handling, such as translating
// certain faults into a recoverable panic on some platforms. Ignoring
// these would not provide any real escape prevention, since they are
// synchronous processor traps triggered by a bug, not a user-initiated
// escape action, so there is nothing for a hostile user to exploit here
// the way there is with SIGINT, SIGTSTP, SIGQUIT, or SIGTERM. Overriding
// their disposition in a Go program is explicitly discouraged by the
// runtime documentation, and could make a genuine crash harder to diagnose
// instead of preventing anything. Blocking these anyway would need a
// signal.Notify handler and a recover strategy, not a blanket Ignore,
// which is a different and riskier feature. No process on Unix, in any
// language, can block SIGKILL or SIGSTOP. Those two are enforced
// unconditionally by the kernel, so "kill -9 <pid>" or "kill -STOP <pid>"
// always works regardless of this function.
func preventEscape() {
	signal.Ignore(
		syscall.SIGINT,  // Ctrl-C
		syscall.SIGTSTP, // Ctrl-Z (suspend to background)
		syscall.SIGQUIT, // Ctrl-\
		syscall.SIGTERM, // plain `kill <pid>`
	)
}

// processCommandLineFlags - This function processes the command line flags
// and prints version or help information as needed.
func processCommandLineFlags() (configFile string, configExplicit, checkConfig bool) {
	defaultConfigFilename := "etc/routercli.yaml"
	sOptConfigFilename := getopt.StringLong("config", 'c', defaultConfigFilename, "The main configuration file", "string")
	bOptHashPassword := getopt.BoolLong("hashpassword", 0, "Create a bcrypt hash suitable for etc/users.yaml")
	bOptCheckConfig := getopt.BoolLong("check-config", 0, "Load and verify the tree structure and Command Levels, then exit")
	bOptHelp := getopt.BoolLong("help", 0, "Help")
	bOptVer := getopt.BoolLong("version", 0, "Version")

	getopt.HelpColumn = 35
	getopt.DisplayWidth = 120
	getopt.SetParameters("")
	getopt.Parse()

	// If the version flag was given, print the version information and
	// exit.
	if *bOptVer {
		printOutputHeader()
		os.Exit(0)
	}

	// If the help flag was given, print the help information and exit.
	if *bOptHelp {
		printOutputHeader()
		getopt.Usage()
		os.Exit(0)
	}

	// If the hashpassword flag was given, run runHashPasswordUtility and
	// exit.
	if *bOptHashPassword {
		runHashPasswordUtility(os.Stdin)
		os.Exit(0)
	}

	// getopt.IsSet reports whether the operator typed --config, as opposed
	// to this function falling back to defaultConfigFilename above. run
	// uses that to decide whether a missing file is an error or an
	// acceptable fall back to built in defaults; see
	// config.LoadSystemConfig and config.LoadSystemConfigOrDefaults.
	return *sOptConfigFilename, getopt.IsSet("config"), *bOptCheckConfig
}

// printOutputHeader - This function prints a header for console output.
func printOutputHeader() {
	fmt.Println("")
	fmt.Println("Router CLI")
	fmt.Println("Copyright: Bret Jordan")
	fmt.Println("Version:", Version)
	if Build != "" {
		fmt.Println("Build:", Build)
	}
	fmt.Println("")
}
