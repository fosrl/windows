//go:build windows

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/fosrl/newt/logger"
)

const (
	AppName            = "Pangolin"
	DefaultHostname    = "https://app.pangolin.net"
	ConfigFileName     = "pangolin.json"
	LogLevel           = "debug" // Centralized log level for the application
	DefaultPrimaryDNS  = "9.9.9.9"
	DefaultDNSOverride = true
)

// Config represents the application configuration
type Config struct {
	UserId       *string `json:"userId,omitempty"`
	Email        *string `json:"email,omitempty"`
	OrgId        *string `json:"orgId,omitempty"`
	Username     *string `json:"username,omitempty"`
	Name         *string `json:"name,omitempty"`
	Hostname     *string `json:"hostname,omitempty"`
	DNSOverride  *bool   `json:"dnsOverride,omitempty"`
	PrimaryDNS   *string `json:"primaryDNS,omitempty"`
	SecondaryDNS *string `json:"secondaryDNS,omitempty"`
}

// ConfigManager manages loading and saving of application configuration
type ConfigManager struct {
	config     *Config
	configPath string
	mu         sync.RWMutex
}

// NewConfigManager creates a new ConfigManager instance
func NewConfigManager() *ConfigManager {
	// Get Local AppData directory (equivalent to Application Support on macOS)
	appData := os.Getenv("LOCALAPPDATA")
	if appData == "" {
		// Fallback to APPDATA if LOCALAPPDATA is not set
		appData = os.Getenv("APPDATA")
	}

	pangolinDir := filepath.Join(appData, AppName)
	configPath := filepath.Join(pangolinDir, ConfigFileName)

	// Create directory if it doesn't exist
	if err := os.MkdirAll(pangolinDir, 0755); err != nil {
		logger.Error("Failed to create config directory: %v", err)
	}

	cm := &ConfigManager{
		configPath: configPath,
	}
	cm.config = cm.load()
	return cm
}

// GetConfig returns the current configuration
func (cm *ConfigManager) GetConfig() *Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config
}

// load loads the configuration from the file
// Returns a default config if the file doesn't exist or can't be read
func (cm *ConfigManager) load() *Config {
	// Check if file exists
	if _, err := os.Stat(cm.configPath); os.IsNotExist(err) {
		return &Config{}
	}

	// Read file
	data, err := os.ReadFile(cm.configPath)
	if err != nil {
		logger.Error("Error loading config: %v", err)
		return &Config{}
	}

	// Parse JSON
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		logger.Error("Error parsing config: %v", err)
		return &Config{}
	}

	return &config
}

// Load loads the configuration from the file
// This is a public method that can be called to reload the config
func (cm *ConfigManager) Load() *Config {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.config = cm.load()
	return cm.config
}

// save saves the configuration to the file without locking
// Caller must hold the lock
func (cm *ConfigManager) save(cfg *Config) bool {
	// Marshal with pretty printing (equivalent to Swift's .prettyPrinted and .sortedKeys)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		logger.Error("Error encoding config: %v", err)
		return false
	}

	// Write to file
	if err := os.WriteFile(cm.configPath, data, 0644); err != nil {
		logger.Error("Error saving config: %v", err)
		return false
	}

	// Update stored config
	cm.config = cfg
	return true
}

// Save saves the configuration to the file
// Returns true if successful, false otherwise
func (cm *ConfigManager) Save(cfg *Config) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.save(cfg)
}

// Clear clears user-specific fields but keeps hostname
// Returns true if successful
func (cm *ConfigManager) Clear() bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	clearedConfig := &Config{}
	if cm.config != nil && cm.config.Hostname != nil {
		clearedConfig.Hostname = cm.config.Hostname
	}

	return cm.save(clearedConfig)
}

// GetHostname returns the hostname from config or the default hostname
func (cm *ConfigManager) GetHostname() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.config != nil && cm.config.Hostname != nil && *cm.config.Hostname != "" {
		return *cm.config.Hostname
	}
	return DefaultHostname
}

// GetDNSOverride returns the DNS override setting from config or the default value
func (cm *ConfigManager) GetDNSOverride() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.config != nil && cm.config.DNSOverride != nil {
		return *cm.config.DNSOverride
	}
	return DefaultDNSOverride
}

// GetPrimaryDNS returns the primary DNS server from config or the default value
func (cm *ConfigManager) GetPrimaryDNS() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.config != nil && cm.config.PrimaryDNS != nil && *cm.config.PrimaryDNS != "" {
		return *cm.config.PrimaryDNS
	}
	return DefaultPrimaryDNS
}

// GetSecondaryDNS returns the secondary DNS server from config or empty string if not set
func (cm *ConfigManager) GetSecondaryDNS() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.config != nil && cm.config.SecondaryDNS != nil {
		return *cm.config.SecondaryDNS
	}
	return ""
}

// SetDNSOverride sets the DNS override setting and saves to config
func (cm *ConfigManager) SetDNSOverride(value bool) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Get current config and copy it to preserve all fields
	cfg := cm.getConfigCopy()
	cfg.DNSOverride = &value
	return cm.save(cfg)
}

// SetPrimaryDNS sets the primary DNS server and saves to config
func (cm *ConfigManager) SetPrimaryDNS(value string) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Get current config and copy it to preserve all fields
	cfg := cm.getConfigCopy()
	cfg.PrimaryDNS = &value
	return cm.save(cfg)
}

// SetSecondaryDNS sets the secondary DNS server and saves to config
func (cm *ConfigManager) SetSecondaryDNS(value string) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Get current config and copy it to preserve all fields
	cfg := cm.getConfigCopy()
	if value == "" {
		cfg.SecondaryDNS = nil // Remove if empty
	} else {
		cfg.SecondaryDNS = &value
	}
	return cm.save(cfg)
}

// getConfigCopy creates a deep copy of the current config
// Caller must hold the lock
func (cm *ConfigManager) getConfigCopy() *Config {
	if cm.config == nil {
		return &Config{}
	}

	// Create a new config and copy all pointer fields
	cfg := &Config{}
	if cm.config.UserId != nil {
		userId := *cm.config.UserId
		cfg.UserId = &userId
	}
	if cm.config.Email != nil {
		email := *cm.config.Email
		cfg.Email = &email
	}
	if cm.config.OrgId != nil {
		orgId := *cm.config.OrgId
		cfg.OrgId = &orgId
	}
	if cm.config.Username != nil {
		username := *cm.config.Username
		cfg.Username = &username
	}
	if cm.config.Name != nil {
		name := *cm.config.Name
		cfg.Name = &name
	}
	if cm.config.Hostname != nil {
		hostname := *cm.config.Hostname
		cfg.Hostname = &hostname
	}
	if cm.config.DNSOverride != nil {
		dnsOverride := *cm.config.DNSOverride
		cfg.DNSOverride = &dnsOverride
	}
	if cm.config.PrimaryDNS != nil {
		primaryDNS := *cm.config.PrimaryDNS
		cfg.PrimaryDNS = &primaryDNS
	}
	if cm.config.SecondaryDNS != nil {
		secondaryDNS := *cm.config.SecondaryDNS
		cfg.SecondaryDNS = &secondaryDNS
	}
	return cfg
}

// GetProgramDataDir returns the base ProgramData directory for the application
// The installer should create this directory and place application files here
func GetProgramDataDir() string {
	return filepath.Join(os.Getenv("PROGRAMDATA"), AppName)
}

// GetLogDir returns the directory path for log files
func GetLogDir() string {
	return filepath.Join(GetProgramDataDir(), "logs")
}

// GetIconsPath returns the directory path for icon files
func GetIconsPath() string {
	return filepath.Join(os.Getenv("PROGRAMFILES"), AppName, "icons")
}
