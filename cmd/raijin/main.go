package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/chat"
	modelconfig "github.com/francescoalemanno/raijin-mono/internal/config"
	"github.com/francescoalemanno/raijin-mono/internal/version"
	bridgecfg "github.com/francescoalemanno/raijin-mono/llmbridge/pkg/config"
)

func main() {
	versionFlag := flag.Bool("version", false, "show version")
	flag.Parse()

	if *versionFlag {
		fmt.Println("raijin " + version.Version)
		os.Exit(0)
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

	if err := run(cfg, strings.Join(os.Args[1:], " ")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
