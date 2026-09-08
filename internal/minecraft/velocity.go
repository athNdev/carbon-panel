package minecraft

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	SettingVelocitySecret = "velocity_forwarding_secret"
)

// GenerateVelocitySecret generates a cryptographically secure 32-byte (256-bit) secret
func GenerateVelocitySecret() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random secret: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// SyncVelocitySecretToDataPath writes or updates the Velocity modern forwarding secret in paper-global.yml or paper.yml
func SyncVelocitySecretToDataPath(dataPath string, secret string) (string, error) {
	if dataPath == "" {
		return "", fmt.Errorf("server data path is empty")
	}

	paperGlobalPath := filepath.Join(dataPath, "config", "paper-global.yml")
	legacyPaperPath := filepath.Join(dataPath, "paper.yml")

	// 1. Check if legacy paper.yml exists and config/paper-global.yml does NOT
	if _, err := os.Stat(legacyPaperPath); err == nil {
		if _, errGlobal := os.Stat(paperGlobalPath); os.IsNotExist(errGlobal) {
			// Update legacy paper.yml
			if err := updateLegacyPaperSecret(legacyPaperPath, secret); err != nil {
				return "", err
			}
			ensureOfflineModeInProperties(dataPath)
			return "paper.yml", nil
		}
	}

	// 2. Default to config/paper-global.yml (modern Paper / Folia / Purpur standard)
	if err := updatePaperGlobalSecret(paperGlobalPath, secret); err != nil {
		return "", err
	}
	ensureOfflineModeInProperties(dataPath)
	return "config/paper-global.yml", nil
}

func updatePaperGlobalSecret(path string, secret string) error {
	configDir := filepath.Dir(path)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	var root map[string]any
	data, err := os.ReadFile(path)
	if err == nil {
		_ = yaml.Unmarshal(data, &root)
	}
	if root == nil {
		root = make(map[string]any)
	}

	proxies, ok := root["proxies"].(map[string]any)
	if !ok {
		proxies = make(map[string]any)
		root["proxies"] = proxies
	}

	velocity, ok := proxies["velocity"].(map[string]any)
	if !ok {
		velocity = make(map[string]any)
		proxies["velocity"] = velocity
	}

	velocity["enabled"] = true
	velocity["online-mode"] = true
	velocity["secret"] = secret

	out, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("failed to marshal paper-global.yml: %w", err)
	}

	return os.WriteFile(path, out, 0644)
}

func updateLegacyPaperSecret(path string, secret string) error {
	var root map[string]any
	data, err := os.ReadFile(path)
	if err == nil {
		_ = yaml.Unmarshal(data, &root)
	}
	if root == nil {
		root = make(map[string]any)
	}

	settings, ok := root["settings"].(map[string]any)
	if !ok {
		settings = make(map[string]any)
		root["settings"] = settings
	}

	velocity, ok := settings["velocity-support"].(map[string]any)
	if !ok {
		velocity = make(map[string]any)
		settings["velocity-support"] = velocity
	}

	velocity["enabled"] = true
	velocity["online-mode"] = true
	velocity["secret"] = secret

	out, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("failed to marshal paper.yml: %w", err)
	}

	return os.WriteFile(path, out, 0644)
}

func ensureOfflineModeInProperties(dataPath string) {
	propsPath := filepath.Join(dataPath, "server.properties")
	props, err := LoadServerProperties(dataPath)
	if err != nil {
		return
	}
	if props["online-mode"] != "false" {
		props["online-mode"] = "false"
		_ = SaveServerProperties(propsPath, props)
	}
}
