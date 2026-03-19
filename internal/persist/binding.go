package persist

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/paths"
)

const (
	SessionBindingKeyEnv      = "RAIJIN_SESSION_BINDING_KEY"
	SessionBindingOwnerPIDEnv = "RAIJIN_SESSION_BINDING_OWNER_PID"
)

var ErrBindingNotFound = errors.New("binding not found")

// Binding tracks the session attached to a bound REPL or shell context.
type Binding struct {
	Key              string `json:"-"`
	SessionID        string `json:"session_id"`
	OwnerPID         int    `json:"owner_pid,omitempty"`
	Ephemeral        bool   `json:"ephemeral,omitempty"`
	SessionCreatedAt int64  `json:"session_created_at,omitempty"`
	SessionUpdatedAt int64  `json:"session_updated_at,omitempty"`
}

func bindingsDir() string {
	return paths.RaijinBindingsDir()
}

func bindingFileName(key string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.TrimSpace(key))) + ".json"
}

func bindingPath(key string) string {
	dir := bindingsDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, bindingFileName(key))
}

// SaveBinding writes a binding record to disk.
func (st *Store) SaveBinding(binding Binding) error {
	key := strings.TrimSpace(binding.Key)
	if key == "" {
		return errors.New("persist: binding key is required")
	}
	if strings.TrimSpace(binding.SessionID) == "" {
		return errors.New("persist: binding session id is required")
	}
	dir := bindingsDir()
	if dir == "" {
		return errors.New("persist: cannot resolve bindings directory")
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("persist: mkdir bindings: %w", err)
	}
	data, err := json.Marshal(binding)
	if err != nil {
		return fmt.Errorf("persist: marshal binding: %w", err)
	}
	if err := os.WriteFile(bindingPath(key), data, filePerm); err != nil {
		return fmt.Errorf("persist: write binding: %w", err)
	}
	return nil
}

// LoadBinding reads a binding record from disk.
func (st *Store) LoadBinding(key string) (Binding, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return Binding{}, ErrBindingNotFound
	}
	data, err := os.ReadFile(bindingPath(key))
	if err != nil {
		if os.IsNotExist(err) {
			return Binding{}, ErrBindingNotFound
		}
		return Binding{}, fmt.Errorf("persist: read binding: %w", err)
	}
	var binding Binding
	if err := json.Unmarshal(data, &binding); err != nil {
		return Binding{}, fmt.Errorf("persist: decode binding: %w", err)
	}
	binding.Key = key
	if strings.TrimSpace(binding.SessionID) == "" {
		return Binding{}, ErrBindingNotFound
	}
	return binding, nil
}

// DeleteBinding removes a binding record from disk.
func (st *Store) DeleteBinding(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	err := os.Remove(bindingPath(key))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("persist: delete binding: %w", err)
	}
	return nil
}
