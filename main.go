package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	modelconfig "github.com/francescoalemanno/raijin-mono/internal/config"
	"github.com/francescoalemanno/raijin-mono/internal/oneshot"
	"github.com/francescoalemanno/raijin-mono/internal/profiling"
	"github.com/francescoalemanno/raijin-mono/internal/shellinit"
	"github.com/francescoalemanno/raijin-mono/internal/version"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func main() {
	versionFlag := flag.Bool("version", false, "show version")
	newFlag := flag.Bool("new", false, "force a new session")
	initFlag := flag.String("init", "", "print shell integration script (zsh, bash, fish)")
	completionsFlag := flag.Bool("completions", false, "print available commands, templates, and skills for shell completion")
	completeFlag := flag.String("complete", "", "print completion candidates for a token")
	fzfModeFlag := flag.String("fzf", "", "run native fzf endpoint (default, complete, paths)")
	fzfQueryFlag := flag.String("fzf-query", "", "set the initial query for --fzf")
	profileDirFlag := flag.String("profile-dir", "", "write live profiling artifacts under this directory")
	pprofAddrFlag := flag.String("pprof-addr", "", "serve runtime pprof on this address (for example 127.0.0.1:6060)")
	flag.Parse()

	if *versionFlag {
		fmt.Println("raijin " + version.Version)
		os.Exit(0)
	}

	if shell := strings.TrimSpace(*initFlag); shell != "" {
		script, err := shellinit.Init(shell)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Print(script)
		os.Exit(0)
	}

	if *completionsFlag {
		fmt.Println(shellinit.Completions())
		os.Exit(0)
	}
	if token := strings.TrimSpace(*completeFlag); token != "" {
		fmt.Println(shellinit.Complete(token))
		os.Exit(0)
	}
	if mode := strings.TrimSpace(*fzfModeFlag); mode != "" {
		code, err := shellinit.RunFZF(mode, strings.TrimSpace(*fzfQueryFlag), os.Stdin, os.Stdout)
		if err != nil {
			fmt.Fprintln(os.Stderr, libagent.FormatErrorForCLI(err))
		}
		os.Exit(code)
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

	store, _ := modelconfig.LoadModelStore()
	modelCfg, runtimeModel := loadDefaultRuntimeModel(store)
	modelCfg = modelCfg.Normalize()

	runtimeModel = buildRuntimeModel(modelCfg, runtimeModel)

	oneShotText := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if oneShotText == "" {
		if err := oneshot.RunSubprocessREPL(os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, libagent.FormatErrorForCLI(err))
			os.Exit(1)
		}
		return
	}

	opts := oneshot.Options{
		RuntimeModel: runtimeModel,
		ModelCfg:     modelCfg,
		Store:        store,
		ForceNew:     *newFlag,
	}
	if err := oneshot.Run(opts, oneShotText); err != nil {
		fmt.Fprintln(os.Stderr, libagent.FormatErrorForCLI(err))
		os.Exit(1)
	}
}

func loadDefaultRuntimeModel(store *modelconfig.ModelStore) (libagent.ModelConfig, libagent.RuntimeModel) {
	if store == nil {
		return libagent.ModelConfig{}, libagent.RuntimeModel{}
	}
	mc, ok := store.GetDefault()
	if !ok {
		return libagent.ModelConfig{}, libagent.RuntimeModel{}
	}
	cfg := mc.Normalize()
	return cfg, buildRuntimeModel(cfg, libagent.RuntimeModel{})
}

func buildRuntimeModel(modelCfg libagent.ModelConfig, current libagent.RuntimeModel) libagent.RuntimeModel {
	provider := strings.TrimSpace(modelCfg.Provider)
	model := strings.TrimSpace(modelCfg.Model)
	if provider == "" || model == "" {
		return current
	}

	apiKey := modelCfg.APIKey
	if after, ok := strings.CutPrefix(apiKey, "$"); ok {
		apiKey = os.Getenv(after)
	}

	cat := libagent.DefaultCatalog()
	resolved, err := cat.NewModel(context.Background(), provider, model, apiKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to configure model (%v); select another model to continue\n", err)
		return current
	}

	info, _, _ := cat.FindModel(provider, model)
	providerType, catalogOpts := cat.FindModelOptions(provider, model)
	return libagent.RuntimeModel{
		Model:                  resolved,
		ModelInfo:              info,
		ModelCfg:               modelCfg,
		ProviderType:           providerType,
		CatalogProviderOptions: catalogOpts,
	}
}
