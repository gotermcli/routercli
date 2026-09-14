// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

// ----------------------------------------------------------------------
// Define Object Model
// ----------------------------------------------------------------------

// State - This holds the values this example's commands mutate and that
// "show running-config" reports back out.
//
// It lives here rather than in package command because it is specific to
// this example. A different command set defines its own. Handlers reach it
// through ctx.State.(*State).
//
// Interfaces is keyed by interface name. "interface eth0" pushes a
// config-if frame carrying that name as its Context, and the handlers
// inside look themselves up here through it. An interface gets no entry
// until something sets a value on it, the same as Description.
//
// TerminalLength and TerminalWidth are not here. Those are session scoped
// and live on AppContext, because Cisco and HP never write them to
// running-config.
//
// Line is different and does belong here. It is the deployment wide
// default a fresh session falls back to, which is what "line vty" and
// "line console" configure on real gear.
type State struct {
	Description string
	Hostname    string
	Interfaces  map[string]*InterfaceState

	// BannerMOTD and BannerLogin hold the two banner texts. Both are shown
	// before authentication.
	//
	// BannerMOTD comes first and is shown whether or not a login prompt
	// follows, since a message of the day is about the connection itself.
	// BannerLogin is shown only immediately before a real login prompt.
	//
	// An empty string, the default, prints nothing.
	BannerMOTD  string
	BannerLogin string

	// Line holds "line" mode's persisted defaults, see LineDefaults and
	// cmd_line.go. Its zero value, every field nil, is the correct state
	// for a deployment where nobody has ever entered "line" mode at all,
	// leaving every one of example/etc/routercli.yaml's config file driven
	// defaults completely untouched.
	Line LineDefaults
}

// LineDefaults - This type holds "line" mode's three settings.
//
// Length and Width are the deployment wide fallbacks used only when a
// session has typed no "terminal length" or "terminal width" and the real
// terminal size cannot be detected. Paging is the deployment wide switch
// for whether the pager runs at all.
//
// Each field is a pointer, with nil meaning the setting has never been
// typed and saved. That is what lets main.go leave a config file driven
// default alone, rather than every boot overwriting it with a zero value
// nobody chose.
//
// It is the same nil-means-unset convention AppContext uses for a
// session's live override, applied one level up to a deployment default.
type LineDefaults struct {
	Length *int
	Width  *int
	Paging *bool
}

// InterfaceState - This type holds the per-interface values config-if mode
// commands mutate. See cmd_interface.go, cmd_description_if.go, and
// cmd_shutdown.go.
type InterfaceState struct {
	Description string
	Shutdown    bool
}
