// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

/*
Package auditlog implements a timestamped, append only record of what each
user did in the CLI. A deployment with real users can answer who ran which
command and whether it succeeded.

Audit logging is turned on at startup or toggled during a session. The
log file is opened lazily, on the first Enable call, so a deployment that
never enables auditing never touches the filesystem for it.
*/
package auditlog
