// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

/*
Package paging implements output filtering and pagination in the style of
Cisco IOS and HP ProCurve devices. It handles pipelines such as "show
running-config | include eth0" and the "--More--" pause for output longer
than one screen.

This package works entirely in terms of lines of text that have already
been produced. It knows nothing about command trees, handler functions, or
translation keys beyond the translator it is given, so it can sit between
any command's output and the terminal.
*/
package paging
