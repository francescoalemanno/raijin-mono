package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/francescoalemanno/raijin-mono/internal/paths"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

const modelsDirPerm = 0o755

type modelsFile struct {
	Default string                          `toml:"default"`
	Models  map[string]libagent.ModelConfig `toml:"models"`
}

type ModelStore struct {
	path string
	data modelsFile
}

func LoadModelStore() (*ModelStore, error) {
	path, err := modelsPath()
	if err != nil {
		return nil, err
	}
	store := &ModelStore{path: path, data: modelsFile{Models: map[string]libagent.ModelConfig{}}}
	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, &store.data); err != nil {
			return nil, err
		}
		if store.data.Models == nil {
			store.data.Models = map[string]libagent.ModelConfig{}
		}
	}
	return store, nil
}

func (s *ModelStore) List() []string {
	names := make([]string, 0, len(s.data.Models))
	for name := range s.data.Models {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s *ModelStore) DefaultName() string {
	return s.data.Default
}

func (s *ModelStore) Get(name string) (libagent.ModelConfig, bool) {
	model, ok := s.data.Models[name]
	return model, ok
}

func (s *ModelStore) GetDefault() (libagent.ModelConfig, bool) {
	if s.data.Default == "" {
		return libagent.ModelConfig{}, false
	}
	model, ok := s.data.Models[s.data.Default]
	return model, ok
}

func (s *ModelStore) Add(model libagent.ModelConfig) error {
	if strings.TrimSpace(model.Name) == "" {
		return fmt.Errorf("model name cannot be empty")
	}
	if s.data.Models == nil {
		s.data.Models = map[string]libagent.ModelConfig{}
	}
	s.data.Models[model.Name] = model
	return s.save()
}

func (s *ModelStore) Delete(name string) error {
	if _, ok := s.data.Models[name]; !ok {
		return fmt.Errorf("model not found: %s", name)
	}
	delete(s.data.Models, name)
	if s.data.Default == name {
		s.data.Default = ""
	}
	return s.save()
}

func (s *ModelStore) SetDefault(name string) error {
	if _, ok := s.data.Models[name]; !ok {
		return fmt.Errorf("model not found: %s", name)
	}
	s.data.Default = name
	return s.save()
}

func (s *ModelStore) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), modelsDirPerm); err != nil {
		return err
	}
	file, err := os.Create(s.path)
	if err != nil {
		return err
	}
	encoder := toml.NewEncoder(file)
	if err := encoder.Encode(s.data); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func modelsPath() (string, error) {
	p := paths.RaijinModelsPath()
	if p == "" {
		return "", fmt.Errorf("failed to resolve config dir")
	}
	return p, nil
}
