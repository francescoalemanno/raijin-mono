package oneshot

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

func handleMaxImages(opts Options, rawArg string) error {
	if opts.Store == nil {
		return errors.New("no model store available")
	}

	defaultName := strings.TrimSpace(opts.Store.DefaultName())
	if defaultName == "" {
		return errors.New("no default model configured; use /add-model first")
	}

	modelCfg, ok := opts.Store.Get(defaultName)
	if !ok {
		return fmt.Errorf("default model not found in store: %s", defaultName)
	}

	arg := strings.TrimSpace(rawArg)
	if arg == "" {
		fmt.Fprintf(stderrWriter, "%s Max images set to %d for %s\n", renderStatusSuccess("✓"), modelCfg.EffectiveMaxImages(), defaultName)
		return nil
	}

	value, err := strconv.Atoi(arg)
	if err != nil || value < 0 {
		return fmt.Errorf("invalid max-images value %q; expected a non-negative integer", arg)
	}

	modelCfg.MaxImages = &value
	if err := opts.Store.Add(modelCfg); err != nil {
		return err
	}

	fmt.Fprintf(stderrWriter, "%s Max images set to %d for %s\n", renderStatusSuccess("✓"), value, defaultName)
	return nil
}
