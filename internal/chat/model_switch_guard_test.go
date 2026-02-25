package chat

import (
	"testing"

	modelconfig "github.com/francescoalemanno/raijin-mono/internal/config"
	bridgecfg "github.com/francescoalemanno/raijin-mono/llmbridge/pkg/config"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
)

func TestApplyModelChoice_BlockedWhileRunning(t *testing.T) {
	app := newChatApp(&chatNoopTerminal{}, nil, &bridgecfg.Config{Providers: map[string]bridgecfg.ProviderConfig{}}, nil)
	store := loadTestModelStore(t)

	oldModel := bridgecfg.SelectedModel{
		Name:          "test/old",
		Provider:      "test",
		APIKey:        "k",
		Model:         "old",
		MaxTokens:     2048,
		ContextWindow: 8192,
	}
	newModel := bridgecfg.SelectedModel{
		Name:          "test/new",
		Provider:      "test",
		APIKey:        "k",
		Model:         "new",
		MaxTokens:     2048,
		ContextWindow: 8192,
	}
	if err := store.Add(oldModel); err != nil {
		t.Fatalf("add old model: %v", err)
	}
	if err := store.Add(newModel); err != nil {
		t.Fatalf("add new model: %v", err)
	}
	if err := store.SetDefault(oldModel.Name); err != nil {
		t.Fatalf("set default old model: %v", err)
	}

	app.dispatchSync(func(_ tui.UIToken) {
		app.store = store
		app.cfg = &bridgecfg.Config{
			Providers: map[string]bridgecfg.ProviderConfig{},
			Model:     oldModel.Normalize(),
		}
		app.state = stateRunning
		app.applyModelChoice(newModel.Name)
	})

	if got := app.cfg.Model.Model; got != oldModel.Model {
		t.Fatalf("active model switched while running: got %q want %q", got, oldModel.Model)
	}
	if got := store.DefaultName(); got != oldModel.Name {
		t.Fatalf("default model switched while running: got %q want %q", got, oldModel.Name)
	}
}

func TestApplyModelAdd_BlockedWhileRunning(t *testing.T) {
	app := newChatApp(&chatNoopTerminal{}, nil, &bridgecfg.Config{Providers: map[string]bridgecfg.ProviderConfig{}}, nil)
	store := loadTestModelStore(t)

	oldModel := bridgecfg.SelectedModel{
		Name:          "test/old",
		Provider:      "test",
		APIKey:        "k",
		Model:         "old",
		MaxTokens:     2048,
		ContextWindow: 8192,
	}
	if err := store.Add(oldModel); err != nil {
		t.Fatalf("add old model: %v", err)
	}
	if err := store.SetDefault(oldModel.Name); err != nil {
		t.Fatalf("set default old model: %v", err)
	}

	app.dispatchSync(func(_ tui.UIToken) {
		app.store = store
		app.cfg = &bridgecfg.Config{
			Providers: map[string]bridgecfg.ProviderConfig{},
			Model:     oldModel.Normalize(),
		}
		app.state = stateRunning
		app.applyModelAdd(ModelAddResult{
			ProviderID:    "newprov",
			ProviderType:  "openai",
			ModelID:       "new-model",
			MaxTokens:     2048,
			ContextWindow: 8192,
			APIKey:        "k2",
		})
	})

	if got := app.cfg.Model.Model; got != oldModel.Model {
		t.Fatalf("active model switched while running: got %q want %q", got, oldModel.Model)
	}
	if _, ok := store.Get("newprov/new-model"); ok {
		t.Fatalf("new model was added while running")
	}
	if got := store.DefaultName(); got != oldModel.Name {
		t.Fatalf("default model switched while running: got %q want %q", got, oldModel.Name)
	}
}

func TestGlobalKeyListener_CtrlPBlockedWhileRunning(t *testing.T) {
	app := newChatApp(&chatNoopTerminal{}, nil, &bridgecfg.Config{Providers: map[string]bridgecfg.ProviderConfig{}}, nil)
	store := loadTestModelStore(t)

	model := bridgecfg.SelectedModel{
		Name:          "test/model",
		Provider:      "test",
		APIKey:        "k",
		Model:         "model",
		MaxTokens:     2048,
		ContextWindow: 8192,
	}
	if err := store.Add(model); err != nil {
		t.Fatalf("add model: %v", err)
	}
	if err := store.SetDefault(model.Name); err != nil {
		t.Fatalf("set default model: %v", err)
	}

	var res *tui.InputListenerResult
	app.dispatchSync(func(_ tui.UIToken) {
		app.store = store
		app.cfg = &bridgecfg.Config{
			Providers: map[string]bridgecfg.ProviderConfig{},
			Model:     model.Normalize(),
		}
		app.state = stateRunning
		res = app.globalKeyListener("\x10") // ctrl+p
		if app.activeModalDone != nil {
			t.Fatalf("model selector should not open while running")
		}
	})

	if res == nil || !res.Consume {
		t.Fatalf("expected ctrl+p to be consumed")
	}
}

func loadTestModelStore(t *testing.T) *modelconfig.ModelStore {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	store, err := modelconfig.LoadModelStore()
	if err != nil {
		t.Fatalf("load model store: %v", err)
	}
	return store
}
