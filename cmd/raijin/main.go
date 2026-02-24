package main

import (
	"fmt"
	"os"
	"strings"

	chat "github.com/francescoalemanno/raijin-mono/internal/chat"
	modelconfig "github.com/francescoalemanno/raijin-mono/internal/config"
	bridgecfg "github.com/francescoalemanno/raijin-mono/llmbridge/pkg/config"
)

func main() {
	cfg, err := bridgecfg.Load()
	if err != nil && err != bridgecfg.ErrConfigNotFound {
		fmt.Fprintln(os.Stderr, "failed to load config:", err)
		os.Exit(1)
	}
	if cfg == nil {
		cfg = bridgecfg.NewConfig()
	}

	if store, err := modelconfig.LoadModelStore(); err == nil && store != nil {
		if modelCfg, ok := store.GetDefault(); ok {
			pc := modelCfg.ToProviderConfig()
			cfg.Providers[pc.ID] = pc
			cfg.Model = modelCfg.ToSelectedModel()
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
