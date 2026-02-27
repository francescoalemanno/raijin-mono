package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/francescoalemanno/raijin-mono/libagent"
	"github.com/francescoalemanno/raijin-mono/internal/artifacts"
	"github.com/francescoalemanno/raijin-mono/internal/paths"
	shellrun "github.com/francescoalemanno/raijin-mono/internal/shell"
)

const (
	localPluginsDir = ".agents/plugins"
)

// pluginMeta is the JSON structure returned by a plugin's --info flag.
type pluginMeta struct {
	Name        string                      `json:"name"`
	Description string                      `json:"description"`
	Parameters  map[string]*libagent.Schema `json:"parameters,omitempty"`
	Required    []string                    `json:"required,omitempty"`
}

type pluginArtifact struct {
	meta       pluginMeta
	scriptPath string
}

func init() {
	artifacts.RegisterLoader(artifacts.KindTools, loadPluginArtifacts)
}

func loadPluginArtifacts() ([]artifacts.Item, error) {
	var userPlugins []pluginArtifact
	var allErrs []error
	if globalDir := paths.UserPluginsDir(); globalDir != "" {
		plugins, errs := discoverPluginArtifacts(globalDir)
		userPlugins = plugins
		allErrs = append(allErrs, errs...)
	}
	projectPlugins, errs := discoverPluginArtifacts(localPluginsDir)
	allErrs = append(allErrs, errs...)

	merged := mergePluginArtifactsByPrecedence(userPlugins, projectPlugins)
	items := make([]artifacts.Item, 0, len(merged))
	for _, plugin := range merged {
		items = append(items, artifacts.Item{
			Kind:  artifacts.KindTools,
			Name:  plugin.meta.Name,
			Value: plugin,
		})
	}
	return items, errors.Join(allErrs...)
}

// pluginTool implements libagent.Tool for an external plugin script.
type pluginTool struct {
	meta       pluginMeta
	scriptPath string
}

func (t *pluginTool) Info() libagent.ToolInfo {
	params := make(map[string]any)
	for name, propSchema := range t.meta.Parameters {
		params[name] = map[string]any(*propSchema)
	}
	required := t.meta.Required
	if required == nil {
		required = []string{}
	}
	return libagent.ToolInfo{
		Name:        t.meta.Name,
		Description: t.meta.Description,
		Parameters:  params,
		Required:    required,
	}
}

func (t *pluginTool) Run(ctx context.Context, params libagent.ToolCall) (libagent.ToolResponse, error) {
	if resp, blocked := toolExecutionGate(ctx, t.meta.Name); blocked {
		return resp, nil
	}

	var stdout, stderr bytes.Buffer
	err := shellrun.Run(ctx, shellrun.ExecSpec{
		Path:  t.scriptPath,
		Stdin: strings.NewReader(params.Input),
	}, &stdout, &stderr)
	if err != nil {
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = err.Error()
		}
		return libagent.NewTextErrorResponse(fmt.Sprintf("plugin %q failed: %s", t.meta.Name, errMsg)), nil
	}

	output := strings.TrimSpace(stdout.String())
	if output == "" {
		output = "(no output)"
	}
	return libagent.NewTextResponse(output), nil
}

func discoverPluginArtifacts(dir string) ([]pluginArtifact, []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []error{fmt.Errorf("plugin dir %q: %w", dir, err)}
	}

	var plugins []pluginArtifact
	var errs []error
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name())

		info, err := entry.Info()
		if err != nil {
			errs = append(errs, fmt.Errorf("plugin %q: stat: %w", path, err))
			continue
		}
		if info.Mode()&0o111 == 0 {
			continue
		}

		meta, err := loadPluginMeta(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("plugin %q: %w", path, err))
			continue
		}
		plugins = append(plugins, pluginArtifact{meta: *meta, scriptPath: path})
	}
	return plugins, errs
}

func newPluginTool(meta pluginMeta, scriptPath string) libagent.Tool {
	renderFunc := func(input json.RawMessage, output string, _ int) string {
		header := fmt.Sprintf("plugin:%s", meta.Name)
		if params := renderPluginParamsPreview(input); params != "" {
			header += " " + params
		}
		if output == "" {
			return header
		}
		return header + "\n" + output
	}

	return WithRender(&pluginTool{
		meta:       meta,
		scriptPath: scriptPath,
	}, renderFunc)
}

func renderPluginParamsPreview(input json.RawMessage) string {
	trimmed := strings.TrimSpace(string(input))
	if trimmed == "" {
		return ""
	}

	var buf bytes.Buffer
	if err := json.Compact(&buf, input); err != nil {
		return strings.Join(strings.Fields(trimmed), " ")
	}

	const maxPreviewRunes = 96
	preview := []rune(buf.String())
	if len(preview) <= maxPreviewRunes {
		return string(preview)
	}
	return string(preview[:maxPreviewRunes-1]) + "…"
}

// LoadPluginTools returns plugin tools from the centralized artifact cache.
func LoadPluginTools() []libagent.Tool {
	cachedPlugins := artifacts.GetAllTyped[pluginArtifact](artifacts.KindTools)
	loaded := make([]libagent.Tool, 0, len(cachedPlugins))
	for _, plugin := range cachedPlugins {
		loaded = append(loaded, newPluginTool(plugin.meta, plugin.scriptPath))
	}
	return loaded
}

// PluginInfo holds metadata about a discovered plugin for system prompt injection.
type PluginInfo struct {
	Name        string
	Description string
}

// LoadPluginInfos returns metadata for all discovered plugins without loading full tools.
func LoadPluginInfos() []PluginInfo {
	cachedPlugins := artifacts.GetAllTyped[pluginArtifact](artifacts.KindTools)
	infos := make([]PluginInfo, 0, len(cachedPlugins))
	for _, plugin := range cachedPlugins {
		infos = append(infos, PluginInfo{
			Name:        plugin.meta.Name,
			Description: plugin.meta.Description,
		})
	}
	return infos
}

// loadPluginMeta runs --info and returns parsed metadata, or an error.
func loadPluginMeta(scriptPath string) (*pluginMeta, error) {
	var stdout, stderr bytes.Buffer
	if err := shellrun.Run(context.Background(), shellrun.ExecSpec{
		Path: scriptPath,
		Args: []string{"--info"},
	}, &stdout, &stderr); err != nil {
		msg := stderr.String()
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("--info failed: %s", msg)
	}

	var meta pluginMeta
	if err := json.Unmarshal(stdout.Bytes(), &meta); err != nil {
		return nil, fmt.Errorf("--info output is not valid JSON: %w", err)
	}
	if meta.Name == "" || meta.Description == "" {
		return nil, errors.New("--info output missing required fields: name, description")
	}
	return &meta, nil
}
