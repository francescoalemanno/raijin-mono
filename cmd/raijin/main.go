package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/chat"
	modelconfig "github.com/francescoalemanno/raijin-mono/internal/config"
	"github.com/francescoalemanno/raijin-mono/internal/theme"
	"github.com/francescoalemanno/raijin-mono/internal/version"
	bridgecfg "github.com/francescoalemanno/raijin-mono/llmbridge/pkg/config"
)

func main() {
	versionFlag := flag.Bool("version", false, "show version")
	themeFlag := flag.String("theme", "dark", "color theme (dark, light)")
	flag.Parse()

	if *versionFlag {
		fmt.Println("raijin " + version.Version)
		os.Exit(0)
	}

	// Set theme at startup
	if !theme.SetTheme(*themeFlag) {
		fmt.Fprintf(os.Stderr, "error: unknown theme %q; available themes: %s\n", *themeFlag, strings.Join(theme.AvailableThemes(), ", "))
		os.Exit(1)
	}

	cfg := bridgecfg.NewConfig()

	if store, err := modelconfig.LoadModelStore(); err == nil && store != nil {
		if modelCfg, ok := store.GetDefault(); ok {
			pc := modelCfg.ToProviderConfig()
			cfg.Providers[pc.ID] = pc
			cfg.Model = modelCfg.Normalize()
		}
	}

	if err := cfg.ConfigureProviders(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to configure providers (%v); select another provider/model to continue\n", err)
	}

	run := chat.RunChatWithPrompt

	if err := run(cfg, strings.Join(flag.Args(), " ")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
