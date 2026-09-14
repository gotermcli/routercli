// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// ----------------------------------------------------------------------
// Define Object Model - Loader
// ----------------------------------------------------------------------

// commandMap - This type is what Command.Subcommands and
// commandTreeFile.Commands decode into, rather than a plain
// map[string]*Command, purely so its own UnmarshalYAML below can run,
// setting each Command's DefIndex as it goes. A commandMap is otherwise
// exactly a map[string]*Command; Go's assignability rules let it stand in
// anywhere an ordinary map[string]*Command is expected, Resolve,
// MergeTrees, HelpText, and every test tree literal in this package's
// tests included, with no conversion needed anywhere else in this package.
type commandMap map[string]*Command

// ----------------------------------------------------------------------
// Public Methods - commandMap
// ----------------------------------------------------------------------

// UnmarshalYAML - Go's map has no order, so once a normal decode returns,
// the order a tree file listed its commands in is gone.
//
// This walks the YAML node content directly, in file order, and stamps
// each decoded Command's DefIndex with its position among its siblings
// before that information is lost.
//
// Each value is decoded through decodeCommandStrict rather than
// node.Decode, so an unknown property inside a command entry is still a
// hard error here, the same as everywhere else.
func (m *commandMap) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("expected a mapping of command name to command, got %s", yamlKindName(node.Kind))
	}

	result := make(commandMap, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode, valNode := node.Content[i], node.Content[i+1]

		var name string
		if err := keyNode.Decode(&name); err != nil {
			return fmt.Errorf("decoding command name: %w", err)
		}

		cmd, err := decodeCommandStrict(valNode)
		if err != nil {
			return fmt.Errorf("command %q: %w", name, err)
		}
		cmd.DefIndex = i / 2
		result[name] = cmd
	}

	*m = result
	return nil
}

// ----------------------------------------------------------------------
// Public Functions - Loader
// ----------------------------------------------------------------------

// LoadTree - This function reads a command tree from a YAML file and
// resolves every "run" directive against the handler registry.
//
// A command that silently does nothing because of a typo is a far worse
// failure than refusing to start, so this fails immediately if the file
// cannot be read or parsed, names a handler nobody registered, or sets a
// property this package does not recognize.
//
// That split is the point: behavior lives in cmd_*.go files that register
// themselves, while structure, what is callable and what it requires,
// lives here in data.
//
// More than one YAML document in a file is also a hard error. A tree file
// is expected to be one top level mapping.
func LoadTree(path string) (map[string]*Command, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading command tree file %q: %w", path, err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var raw commandTreeFile
	if err := dec.Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			// The file is empty, or contains comments only. An empty tree,
			// zero commands, is unusual but not itself an error.
			return nil, nil
		}
		return nil, fmt.Errorf("error parsing command tree file %q: %w", path, err)
	}

	var extra commandTreeFile
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("command tree file %q contains multiple YAML documents", path)
		}
		return nil, fmt.Errorf("error parsing command tree file %q: %w", path, err)
	}

	if err := resolveHandlers(raw.Commands, ""); err != nil {
		return nil, fmt.Errorf("error building command tree from %q: %w", path, err)
	}
	return raw.Commands, nil
}

// ----------------------------------------------------------------------
// Private Functions - Loader
// ----------------------------------------------------------------------

// resolveHandlers - This function recursively resolves every command's Run
// into its actual RunFunc handler by looking it up in the plugin registry.
// Every other field on Command already came through decoding directly, see
// Command's doc comment, so this is the one thing left for a tree to do
// after yaml.Unmarshal returns. pathSoFar exists so an error message can
// point at the actual failing command instead of just naming a handler
// string with no context for where it went wrong. A command with an empty
// Run, a pure container command with only Subcommands, is left with a nil
// RunFunc, exactly as intended.
func resolveHandlers(tree map[string]*Command, pathSoFar string) error {
	for name, c := range tree {
		commandPath := name
		if pathSoFar != "" {
			commandPath = pathSoFar + " " + name
		}

		if c.Run != "" {
			fn, ok := lookupHandler(c.Run)
			if !ok {
				return fmt.Errorf("command %q references handler %q, but nothing registered that name (check the command's init() call)", commandPath, c.Run)
			}
			c.RunFunc = fn
		}

		if err := resolveHandlers(c.Subcommands, commandPath); err != nil {
			return err
		}
	}
	return nil
}

// decodeCommandStrict - This function decodes one command entry with
// unknown YAML keys treated as a hard error, matching what LoadTree does
// on its top level decoder.
//
// A yaml.Node's own Decode has no KnownFields option, so this re-marshals
// the node back to text and decodes that through a fresh decoder purely to
// turn the check on.
//
// It runs once per command entry at startup, a few dozen times at most, so
// the extra round trip costs nothing that matters.
func decodeCommandStrict(node *yaml.Node) (*Command, error) {
	data, err := yaml.Marshal(node)
	if err != nil {
		return nil, fmt.Errorf("re-marshaling command node: %w", err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var cmd Command
	if err := dec.Decode(&cmd); err != nil {
		return nil, err
	}
	return &cmd, nil
}

// yamlKindName - This function names a yaml.Kind for use in
// commandMap.UnmarshalYAML's error message, the same reasoning
// config.durationKindName already follows for Duration's UnmarshalYAML.
func yamlKindName(k yaml.Kind) string {
	switch k {
	case yaml.DocumentNode:
		return "document"
	case yaml.MappingNode:
		return "mapping"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.ScalarNode:
		return "scalar"
	case yaml.AliasNode:
		return "alias"
	default:
		return fmt.Sprintf("kind %d", int(k))
	}
}
