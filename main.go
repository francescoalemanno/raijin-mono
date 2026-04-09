package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	modelconfig "github.com/francescoalemanno/raijin-mono/internal/config"
	"github.com/francescoalemanno/raijin-mono/internal/oneshot"
	"github.com/francescoalemanno/raijin-mono/internal/persist"
	"github.com/francescoalemanno/raijin-mono/internal/profiling"
	"github.com/francescoalemanno/raijin-mono/internal/shellinit"
	"github.com/francescoalemanno/raijin-mono/internal/version"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func main() {
	parsedArgs, newFlag := extractNewFlag(os.Args[1:])

	versionFlag := flag.Bool("version", false, "show version")
	_ = flag.String("new", "", "start a new REPL session; optionally submit the first prompt")
	ephemeralFlag := flag.Bool("ephemeral", false, "run a one-shot prompt without loading or persisting session history")
	noThinkingFlag := flag.Bool("no-thinking", false, "suppress printing of reasoning/thinking blocks to terminal")
	noEchoFlag := flag.Bool("no-echo", false, "suppress echoing user prompts to terminal")
	initFlag := flag.String("init", "", "print shell integration script (zsh, bash, fish)")
	completionsFlag := flag.Bool("completions", false, "print available commands, templates, and skills for shell completion")
	completeFlag := flag.String("complete", "", "resolve completion for a token or input line (interactive fzf)")
	completeListFlag := flag.String("complete-list", "", "list completion candidates for a token or input line (for shell integration)")
	removeModelFlag := flag.String("remove-model", "", "remove configured model by name")
	removeSessionFlag := flag.String("remove-session", "", "remove persisted session by id (full or short)")
	profileDirFlag := flag.String("profile-dir", "", "write live profiling artifacts under this directory")
	pprofAddrFlag := flag.String("pprof-addr", "", "serve runtime pprof on this address (for example 127.0.0.1:6060)")
	if err := flag.CommandLine.Parse(parsedArgs); err != nil {
		fmt.Fprintln(os.Stderr, libagent.FormatErrorForCLI(err))
		os.Exit(2)
	}

	if *versionFlag {
		fmt.Println("raijin " + version.Version)
		os.Exit(0)
	}

	if shell := strings.TrimSpace(*initFlag); shell != "" {
		exe, err := os.Executable()
		if err != nil {
			exe = "raijin"
		}
		script, err := shellinit.Init(shell, exe)
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
		fmt.Println(shellinit.CompleteSelection(token))
		os.Exit(0)
	}
	if token := strings.TrimSpace(*completeListFlag); token != "" {
		fmt.Print(shellinit.Complete(token))
		os.Exit(0)
	}
	didDelete := false
	if modelName := strings.TrimSpace(*removeModelFlag); modelName != "" {
		if err := removeModelByName(modelName); err != nil {
			fmt.Fprintln(os.Stderr, libagent.FormatErrorForCLI(err))
			os.Exit(1)
		}
		fmt.Printf("Removed model: %s\n", modelName)
		didDelete = true
	}
	if sessionRef := strings.TrimSpace(*removeSessionFlag); sessionRef != "" {
		shortID, err := removeSessionByRef(sessionRef)
		if err != nil {
			fmt.Fprintln(os.Stderr, libagent.FormatErrorForCLI(err))
			os.Exit(1)
		}
		fmt.Printf("Removed session: %s\n", shortID)
		didDelete = true
	}
	if didDelete {
		os.Exit(0)
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

	oneShotText := strings.TrimSpace(strings.Join(flag.CommandLine.Args(), " "))
	if newFlag.present && oneShotText != "" {
		fmt.Fprintln(os.Stderr, "-new starts REPL mode and does not accept positional prompts; use -new=\"your prompt\"")
		os.Exit(1)
	}

	if oneShotText == "" {
		if *ephemeralFlag {
			fmt.Fprintln(os.Stderr, "--ephemeral is only supported for one-shot prompts")
			os.Exit(1)
		}
		if err := oneshot.RunSubprocessREPL(parsedArgs, newFlag.prompt, newFlag.present); err != nil {
			fmt.Fprintln(os.Stderr, libagent.FormatErrorForCLI(err))
			os.Exit(1)
		}
		return
	}

	opts := oneshot.Options{
		RuntimeModel: runtimeModel,
		ModelCfg:     modelCfg,
		Store:        store,
		ForceNew:     newFlag.present,
		Ephemeral:    *ephemeralFlag,
		NoThinking:   *noThinkingFlag,
		NoEcho:       *noEchoFlag,
	}
	if err := oneshot.Run(opts, oneShotText); err != nil {
		fmt.Fprintln(os.Stderr, libagent.FormatErrorForCLI(err))
		os.Exit(1)
	}
}

type newFlagValue struct {
	present bool
	prompt  string
}

func extractNewFlag(args []string) ([]string, newFlagValue) {
	out := make([]string, 0, len(args))
	var newFlag newFlagValue

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			out = append(out, args[i:]...)
			return out, newFlag
		case arg == "-new" || arg == "--new":
			newFlag.present = true
			newFlag.prompt = ""
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				newFlag.prompt = args[i+1]
				i++
			}
		case strings.HasPrefix(arg, "-new="):
			newFlag.present = true
			newFlag.prompt = strings.TrimPrefix(arg, "-new=")
		case strings.HasPrefix(arg, "--new="):
			newFlag.present = true
			newFlag.prompt = strings.TrimPrefix(arg, "--new=")
		default:
			out = append(out, arg)
		}
	}

	return out, newFlag
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

func removeModelByName(name string) error {
	store, err := modelconfig.LoadModelStore()
	if err != nil {
		return err
	}
	if store == nil {
		return errors.New("no model store available")
	}
	return store.Delete(name)
}

func removeSessionByRef(ref string) (string, error) {
	store, err := persist.OpenStore()
	if err != nil {
		return "", err
	}
	summaries := store.ListSessionSummaries()
	if len(summaries) == 0 {
		return "", errors.New("no previous sessions found")
	}

	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errors.New("session id cannot be empty")
	}

	var exact *persist.SessionSummary
	for i := range summaries {
		if summaries[i].ID == ref {
			exact = &summaries[i]
			break
		}
	}
	if exact != nil {
		return exact.ShortID, store.RemoveSession(exact.ID)
	}

	matches := make([]persist.SessionSummary, 0, 1)
	for _, summary := range summaries {
		if summary.ShortID == ref {
			matches = append(matches, summary)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("session not found: %s", ref)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("short session id is ambiguous: %s", ref)
	}
	return matches[0].ShortID, store.RemoveSession(matches[0].ID)
}
