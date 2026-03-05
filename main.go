package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"

	"github.com/francescoalemanno/raijin-mono/internal/chat"
	modelconfig "github.com/francescoalemanno/raijin-mono/internal/config"
	"github.com/francescoalemanno/raijin-mono/internal/profiling"
	"github.com/francescoalemanno/raijin-mono/internal/theme"
	"github.com/francescoalemanno/raijin-mono/internal/version"
)

func main() {
	versionFlag := flag.Bool("version", false, "show version")
	themeFlag := flag.String("theme", "dark", "color theme (dark, light)")
	oneShotPrompt := flag.String("p", "", "run one-shot prompt in CLI mode")
	profileDirFlag := flag.String("profile-dir", "", "write live profiling artifacts under this directory")
	pprofAddrFlag := flag.String("pprof-addr", "", "serve runtime pprof on this address (for example 127.0.0.1:6060)")
	flag.Parse()

	if *versionFlag {
		fmt.Println("raijin " + version.Version)
		os.Exit(0)
	}

	if !theme.SetTheme(*themeFlag) {
		fmt.Fprintf(os.Stderr, "error: unknown theme %q; available themes: %s\n", *themeFlag, strings.Join(theme.AvailableThemes(), ", "))
		os.Exit(1)
	}

	profiler, err := profiling.Start(profiling.Options{
		Dir:       strings.TrimSpace(*profileDirFlag),
		PprofAddr: strings.TrimSpace(*pprofAddrFlag),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to start profiling: %v\n", err)
		os.Exit(1)
	}
	if profiler.Enabled() {
		if dir := profiler.Dir(); dir != "" {
			fmt.Fprintf(os.Stderr, "profiling capture enabled: %s\n", dir)
		}
		if addr := profiler.PprofAddr(); addr != "" {
			fmt.Fprintf(os.Stderr, "pprof endpoint enabled: http://%s/debug/pprof/\n", addr)
		}
		defer func() {
			if stopErr := profiler.Stop(); stopErr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to stop profiling cleanly: %v\n", stopErr)
			}
		}()
	}

	var runtimeModel libagent.RuntimeModel
	var modelCfg libagent.ModelConfig

	if store, err := modelconfig.LoadModelStore(); err == nil && store != nil {
		if mc, ok := store.GetDefault(); ok {
			modelCfg = mc.Normalize()
			apiKey := modelCfg.APIKey
			if strings.HasPrefix(apiKey, "$") {
				apiKey = os.Getenv(strings.TrimPrefix(apiKey, "$"))
			}
			cat := libagent.DefaultCatalog()
			if model, err := cat.NewModel(context.Background(), modelCfg.Provider, modelCfg.Model, apiKey); err == nil {
				info, _, _ := cat.FindModel(modelCfg.Provider, modelCfg.Model)
				providerType, catalogOpts := cat.FindModelOptions(modelCfg.Provider, modelCfg.Model)
				runtimeModel = libagent.RuntimeModel{
					Model:                  model,
					ModelInfo:              info,
					ModelCfg:               modelCfg,
					ProviderType:           providerType,
					CatalogProviderOptions: catalogOpts,
				}
			} else {
				fmt.Fprintf(os.Stderr, "warning: failed to configure model (%v); select another model to continue\n", err)
			}
		}
	}

	oneShot := strings.TrimSpace(*oneShotPrompt)
	if oneShot != "" {
		if len(flag.Args()) > 0 {
			fmt.Fprintln(os.Stderr, "error: positional prompt arguments cannot be combined with -p")
			os.Exit(1)
		}
		response, err := chat.RunOneShot(runtimeModel, modelCfg, oneShot)
		if err != nil {
			fmt.Fprintln(os.Stderr, libagent.FormatErrorForCLI(err))
			os.Exit(1)
		}
		fmt.Println(response)
		return
	}

	if err := chat.RunChatWithPrompt(runtimeModel, modelCfg, strings.Join(flag.Args(), " ")); err != nil {
		fmt.Fprintln(os.Stderr, libagent.FormatErrorForCLI(err))
		os.Exit(1)
	}
}
