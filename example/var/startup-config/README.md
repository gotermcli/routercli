# var/startup-config

This directory holds the saved startup-config file, the same text file
Cisco and HP call startup-config, written by "write memory" and
removed by "erase startup-config", both in the admin Command Level,
cmd/product/cmd_startup_config.go, and read back in automatically at
every process start by command.LoadStartupConfig in
command/replay.go. See command.AppContext.ReplayingStartupConfig's
doc comment in command/model.go for the reasoning behind why that
automatic replay needs no password of its own, and etc/README.md's
StartupConfigFile section for how a deployment points routercli.yaml
at a file, and a directory, other than this one.

This directory ships empty on a fresh checkout, matching a fresh
device that has never had "write memory" run against it. The actual
saved file, StartupConfigFile's filename, "startup-config" by
default, is created the first time that command runs, and is not meant
to be checked into source control alongside this README, since it
holds a live deployment's saved state, not anything this framework
itself ships.

This README exists so the directory itself, empty otherwise, survives
a checkout the same way var/log's log files, and var/lang's and
var/tree's real content, already keep those directories from
disappearing. Git does not track empty directories on its own.
