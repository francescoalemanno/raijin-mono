package libagent

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/francescoalemanno/raijin-mono/libagent/oauth"
)

// credentialsFilePath returns the path to the persisted OAuth credentials file:
// ~/.config/libagent/oauth_credentials.json on every OS.
func credentialsFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "libagent", "oauth_credentials.json"), nil
}

// loadCredentials reads the credentials file and returns its contents.
// Returns an empty map (not an error) when the file does not exist yet.
func loadCredentials() (map[string]oauth.Credentials, error) {
	path, err := credentialsFilePath()
	if err != nil {
		return make(map[string]oauth.Credentials), err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return make(map[string]oauth.Credentials), nil
	}
	if err != nil {
		return make(map[string]oauth.Credentials), err
	}

	var store map[string]oauth.Credentials
	if err := json.Unmarshal(data, &store); err != nil {
		return make(map[string]oauth.Credentials), err
	}
	return store, nil
}

// saveCredentials writes the full credentials map to the credentials file
// atomically (write to a temp file, then rename).
func saveCredentials(store map[string]oauth.Credentials) error {
	path, err := credentialsFilePath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}

	// Write to a temp file in the same directory then rename for atomicity.
	tmp, err := os.CreateTemp(filepath.Dir(path), "oauth_credentials_*.json.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}
