// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

// ----------------------------------------------------------------------
// Public Methods - State
// ----------------------------------------------------------------------

// Interface - This method returns the InterfaceState for name, creating
// one, and the Interfaces map itself if this is the very first interface
// touched, if it does not exist yet. Centralizing this lazy creation here,
// instead of repeating a nil check in every config-if handler, is what
// keeps cmd_description_if.go and cmd_shutdown.go each a few lines long.
func (s *State) Interface(name string) *InterfaceState {
	if s.Interfaces == nil {
		s.Interfaces = make(map[string]*InterfaceState)
	}
	iface, ok := s.Interfaces[name]
	if !ok {
		iface = &InterfaceState{}
		s.Interfaces[name] = iface
	}
	return iface
}
