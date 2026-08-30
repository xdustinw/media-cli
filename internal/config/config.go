// Package config wires Viper configuration for the CLI.
//
// Precedence (highest first): explicit flags, environment variables prefixed
// with MC_, values from a config file, then defaults registered here.
package config

import (
	"strings"

	"github.com/spf13/viper"
)

// Keys used across the application. Cobra flags bind to these in the various
// command init() functions.
const (
	KeyVerbose     = "verbose"
	KeyDebug       = "debug"
	KeyAssumeYes   = "assume_yes"
	KeyMediaExts   = "media.extensions"
	KeyHashMetaKey = "hash.metadata_key"
	KeyHashNameLen = "hash.name_length"
)

// New returns a fresh Viper instance with defaults and environment binding
// applied. It is kept on the root command and shared with sub-commands.
func New() *viper.Viper {
	v := viper.New()

	v.SetEnvPrefix("MC")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	v.SetDefault(KeyVerbose, false)
	v.SetDefault(KeyDebug, false)
	v.SetDefault(KeyAssumeYes, false)
	v.SetDefault(KeyMediaExts, []string{
		".mp4", ".mkv", ".mov", ".m4v", ".webm", ".avi", // video
		".jpg", ".jpeg", ".jpe", ".jfif", ".png", ".apng", ".gif", ".webp", // images
	})
	v.SetDefault(KeyHashMetaKey, "mc.hash")
	v.SetDefault(KeyHashNameLen, 6)

	return v
}

// Load reads an optional config file. A missing file is not an error; any other
// failure (malformed file, unreadable path) is returned to the caller.
func Load(v *viper.Viper, explicitPath string) error {
	if explicitPath != "" {
		v.SetConfigFile(explicitPath)
	} else {
		v.SetConfigName("media-cli")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("$HOME/.config/media-cli")
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return nil
		}
		// An explicitly requested file that does not exist surfaces here too.
		return err
	}
	return nil
}
