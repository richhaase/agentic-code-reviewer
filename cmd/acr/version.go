package main

import (
	"fmt"
	"runtime/debug"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func buildVersionString() string {
	ver, rev, buildDate := getVersionInfo()
	return fmt.Sprintf("acr %s (commit: %s, built: %s)", ver, rev, buildDate)
}

func getVersionInfo() (ver, rev, buildDate string) {
	ver, rev, buildDate = version, commit, date

	if ver == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok {
			if info.Main.Version != "" && info.Main.Version != "(devel)" {
				ver = info.Main.Version
			}
			for _, setting := range info.Settings {
				switch setting.Key {
				case "vcs.revision":
					if len(setting.Value) >= 7 {
						rev = setting.Value[:7]
					} else if setting.Value != "" {
						rev = setting.Value
					}
				case "vcs.time":
					if setting.Value != "" {
						buildDate = setting.Value
					}
				case "vcs.modified":
					if setting.Value == "true" && rev != "none" {
						rev += "-dirty"
					}
				}
			}
		}
	}

	return ver, rev, buildDate
}
