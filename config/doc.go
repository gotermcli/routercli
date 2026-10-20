// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

/*
Package config implements loading and validating the RouterCLI system
configuration file. It is the single place that says where the tree files
live, whether a login is required, and how strict the rate limiting is,
so none of that is hardcoded in a deployment's main.go.

The configuration file is YAML, decoded with strict unknown field
checking, so a typo in a property name is a startup error rather than a
silently ignored setting. A configuration file named explicitly on the
command line MUST exist and MUST be valid. The built in default path is
allowed to be absent, in which case DefaultSystemConfig is used, so a
deployment can run before it has written a configuration file.
*/
package config
