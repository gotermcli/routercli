// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

// Command routerclid is the multiuser daemon. Without it, each CLI session
// holds its own copy of the running configuration, the user database, and
// the role set, and those drift apart as soon as two sessions change
// different things.
//
// This process holds one canonical copy and every attached session shares
// it. It is opt in through DaemonSocketPath.
//
// It never touches a terminal. The routercli binary keeps doing its own
// terminal I/O; this one talks only to CLI clients over
// a Unix domain socket.
//
// The binary itself is mostly wiring: load the same example/etc/routercli.yaml a
// CLI process would, build the initial state from it the way replaying a
// startup-config does, open the socket, and hand the rest to
// daemon.Server, which owns the wire protocol, session tracking, and
// reboot behavior.
//
// SIGTERM stops it. SIGHUP rereads configuration and reboots every
// attached session.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/gotermcli/routercli/auditlog"
	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/command"
	"github.com/gotermcli/routercli/config"
	"github.com/gotermcli/routercli/daemon"
	_ "github.com/gotermcli/routercli/example/cmd/core"
	"github.com/gotermcli/routercli/example/cmd/product"
	_ "github.com/gotermcli/routercli/example/cmd/session"

	"github.com/gologme/log"
	"github.com/pborman/getopt/v2"
)

// main - This function is nothing but the call to run and the process exit
// status.
//
// os.Exit does not run deferred functions, so a startup failure inside one
// large main would skip every defer registered before it, including
// store.Close and sessions.Close. Those are what stop the writer goroutine
// and the session directory cleanly.
//
// Returning an error out of run instead lets every defer run, whichever
// step failed.
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run - This function performs every startup step in order, then serves
// connections until told to stop. It returns nil on a clean shutdown and
// an error on any startup failure.
func run() error {
	defaultConfigFilename := "etc/routercli.yaml"
	sOptConfigFilename := getopt.StringLong("config", 'c', defaultConfigFilename, "The main configuration file", "string")
	getopt.HelpColumn = 35
	getopt.DisplayWidth = 120
	getopt.SetParameters("")
	getopt.Parse()

	// A configuration file the operator named explicitly must exist and
	// must be valid; a typo in --config fails here rather than starting
	// this daemon on defaults nobody asked for. The built in default path
	// is allowed to be missing, matching the CLI's behavior.
	load := config.LoadSystemConfigOrDefaults
	if getopt.IsSet("config") {
		load = config.LoadSystemConfig
	}
	cfg, err := load(*sOptConfigFilename)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}
	if cfg.DaemonSocketPath == "" {
		return fmt.Errorf("this deployment has no DaemonSocketPath configured in %s, routerclid has nothing to listen on", *sOptConfigFilename)
	}

	logOutput := io.Writer(os.Stderr)
	if cfg.LogFile != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0750); err != nil {
			fmt.Fprintln(os.Stderr, "warning: failed to prepare log file directory, falling back to stderr:", err)
		} else if f, err := os.OpenFile(cfg.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640); err != nil {
			fmt.Fprintln(os.Stderr, "warning: failed to open LogFile, falling back to stderr:", err)
		} else {
			logOutput = f
			defer f.Close()
		}
	}
	logger := log.New(logOutput, "routerclid ", log.LstdFlags)
	switch cfg.LogLevel {
	case 1:
		logger.EnableLevel("error")
		logger.EnableLevel("info")
	case 3:
		logger.EnableLevel("error")
		logger.EnableLevel("info")
		logger.EnableLevel("warn")
	case 5:
		logger.EnableLevel("error")
		logger.EnableLevel("info")
		logger.EnableLevel("warn")
		logger.EnableLevel("debug")
	}
	if os.Getenv("ROUTERCLI_DEBUG") != "" {
		logger.EnableLevel("debug")
	}

	audit := auditlog.New(cfg.AuditLogFile, logger)
	if cfg.AuditLogEnabled {
		if err := os.MkdirAll(filepath.Dir(cfg.AuditLogFile), 0750); err != nil {
			return fmt.Errorf("failed to prepare audit log directory: %w", err)
		}
		if err := audit.Enable(); err != nil {
			return fmt.Errorf("failed to open audit log: %w", err)
		}
	}
	defer audit.Close()

	reload := func() (daemon.State, error) {
		return buildDaemonState(&cfg, logger)
	}
	initialState, err := reload()
	if err != nil {
		return fmt.Errorf("failed to build initial state: %w", err)
	}

	store := daemon.NewStore(initialState)
	defer store.Close()

	sessions := daemon.NewSessionDirectory()
	defer sessions.Close()

	keyPath := daemon.StaticKeyPath(cfg.DaemonSocketPath)
	staticPrivate, err := daemon.LoadOrCreateStaticKeyPair(keyPath)
	if err != nil {
		return fmt.Errorf("failed to load or create this daemon's static key pair: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(cfg.DaemonSocketPath), 0750); err != nil {
		return fmt.Errorf("failed to prepare socket directory: %w", err)
	}
	// AllowedUIDs is seeded with only this daemon's effective UID, which is
	// the minimum every deployment needs.
	//
	// A deployment wanting more than one local account to reach this socket
	// arranges that through operating system account and group setup around
	// socketPermissions. There is no RouterCLI specific allow list exposed
	// through configuration.
	checker := daemon.NewAllowedUIDs(uint32(os.Getuid()))
	listener, err := daemon.Listen(cfg.DaemonSocketPath, checker)
	if err != nil {
		return fmt.Errorf("failed to open daemon socket: %w", err)
	}

	server := daemon.NewServer(listener, staticPrivate, store, sessions, audit, reload, logger)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for s := range sig {
			switch s {
			case syscall.SIGHUP:
				logger.Infoln("SIGHUP received, rereading configuration and rebroadcasting")
				if err := server.TriggerReboot(); err != nil {
					logger.Errorf("SIGHUP reboot failed: %v", err)
				}
			case syscall.SIGTERM:
				logger.Infoln("SIGTERM received, draining sessions and stopping")
				if err := server.Shutdown(); err != nil {
					logger.Errorf("Shutdown: %v", err)
				}
				return
			}
		}
	}()

	logger.Infoln("routerclid listening on", cfg.DaemonSocketPath)
	server.Serve()
	logger.Infoln("routerclid stopped")

	return nil
}

// buildDaemonState - This function loads the tree structure, role set,
// and, when AuthRequired is on, the user database, the same files a
// standalone CLI process loads at startup. It then replays
// StartupConfigFile to produce a fresh product state and a fresh set of
// runtime defined aliases and level passwords.
//
// It runs once at daemon startup and again on every reboot, so the state
// always reflects what is on disk right now rather than a stale copy.
//
// The replay runs against a throwaway AppContext whose DaemonClient is a
// StandaloneClient wrapping exactly the values this function is about to
// return. That is needed because a replayed line such as "hostname" goes
// through MutateProductState rather than touching ctx.State directly.
//
// That client is closed before this returns. Nothing about it survives the
// replay.
func buildDaemonState(cfg *config.SystemConfig, logger *log.Logger) (daemon.State, error) {
	levels, err := command.LoadTreeStructure(cfg.TreeStructure, cfg.CommonTreeFile)
	if err != nil {
		return daemon.State{}, fmt.Errorf("loading tree structure: %w", err)
	}
	for _, level := range levels.Order {
		level.RateLimiter = auth.NewRateLimiter(cfg.CommandLevelMaxAttempts, cfg.CommandLevelAttemptWindow.AsDuration(), cfg.CommandLevelLockoutDuration.AsDuration())
	}
	if problems := command.VerifyCommandLevels(levels); len(problems) > 0 {
		return daemon.State{}, fmt.Errorf("invalid tree structure: %w", errors.Join(problems...))
	}
	if problems := command.VerifyVendorDefinedSecrets(levels); len(problems) > 0 {
		return daemon.State{}, fmt.Errorf("invalid tree structure: %w", errors.Join(problems...))
	}

	roles, err := command.LoadRoles(cfg.RolesFile)
	if err != nil {
		return daemon.State{}, fmt.Errorf("loading roles: %w", err)
	}
	if problems := command.VerifyRoles(levels, roles); len(problems) > 0 {
		return daemon.State{}, fmt.Errorf("invalid roles: %w", errors.Join(problems...))
	}

	users := auth.Users{}
	if cfg.AuthRequired {
		users, err = auth.LoadUsers(cfg.UsersFile)
		if err != nil {
			return daemon.State{}, fmt.Errorf("loading users: %w", err)
		}
	}

	productState := &product.State{}
	standalone := daemon.NewStandaloneClient(daemon.NewState(productState, levels, users, roles, cfg))
	defer standalone.Close()

	base := levels.Base()
	replayCtx := &command.AppContext{
		State:             productState,
		DaemonClient:      standalone,
		Logger:            logger,
		Levels:            levels,
		Roles:             roles,
		Users:             users,
		Session:           &auth.Session{CommandLevel: base.Name},
		Position:          command.NewCommandLevelStack(base.Name, base.PromptSuffix, base.Tree),
		StartupConfigFile: cfg.StartupConfigFile,
		RolesFile:         cfg.RolesFile,
	}

	if err := os.MkdirAll(filepath.Dir(cfg.StartupConfigFile), 0750); err != nil {
		return daemon.State{}, fmt.Errorf("preparing startup-config directory: %w", err)
	}
	if err := command.LoadStartupConfig(replayCtx, cfg.StartupConfigFile); err != nil {
		return daemon.State{}, fmt.Errorf("replaying startup-config: %w", err)
	}

	return daemon.NewState(productState, levels, users, roles, cfg), nil
}
