//go:build windows

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"github.com/fosrl/newt/logger"
	"golang.org/x/sys/windows"
)

const (
	AppName            = "Pangolin"
	DefaultHostname    = "https://app.pangolin.net"
	ConfigFileName     = "pangolin.json"
	LogLevel           = "debug" // Centralized log level for the application
	DefaultPrimaryDNS  = "9.9.9.9"
	DefaultDNSOverride = true
	DefaultDNSTunnel   = false
)

// Config represents the application configuration
type Config struct {
	DNSOverride  *bool   `json:"dnsOverride,omitempty"`
	DNSTunnel    *bool   `json:"dnsTunnel,omitempty"`
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
	if err := os.MkdirAll(pangolinDir, 0o755); err != nil {
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
	if err := os.WriteFile(cm.configPath, data, 0o644); err != nil {
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

// Clear clears user-specific fields
// Returns true if successful
func (cm *ConfigManager) Clear() bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	clearedConfig := &Config{}

	return cm.save(clearedConfig)
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

// GetDNSTunnel returns the DNS tunnel setting from config or false if not set
func (cm *ConfigManager) GetDNSTunnel() bool {
    cm.mu.RLock()
    defer cm.mu.RUnlock()

    if cm.config != nil && cm.config.DNSTunnel != nil {
        return *cm.config.DNSTunnel
    }
    return DefaultDNSTunnel
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

// SetDNSTunnel sets the DNS tunnel setting and saves to config
func (cm *ConfigManager) SetDNSTunnel(value bool) bool {
    cm.mu.Lock()
    defer cm.mu.Unlock()

    // Get current config and copy it to preserve all fields
    cfg := cm.getConfigCopy()
    cfg.DNSTunnel = &value
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
	if cm.config.DNSOverride != nil {
		dnsOverride := *cm.config.DNSOverride
		cfg.DNSOverride = &dnsOverride
	}
	if cm.config.DNSTunnel != nil {
        dnsTunnel := *cm.config.DNSTunnel
        cfg.DNSTunnel = &dnsTunnel
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

// GetFriendlyDeviceName returns a friendly device name like "Windows Laptop" or "Windows Desktop"
// It attempts to detect the device type by checking for battery presence
func GetFriendlyDeviceName() string {
	// Check if system has a battery (indicates laptop)
	hasBattery := isLaptop()

	if hasBattery {
		return "Windows Laptop"
	}
	return "Windows Desktop"
}

// isLaptop attempts to determine if the system is a laptop by checking for battery presence
func isLaptop() bool {
	// Use GetSystemPowerStatus to check if battery is present
	// This is a simple heuristic: laptops typically have batteries
	var status struct {
		ACLineStatus        byte
		BatteryFlag         byte
		BatteryLifePercent  byte
		Reserved1           byte
		BatteryLifeTime     uint32
		BatteryFullLifeTime uint32
	}

	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	getSystemPowerStatus := kernel32.NewProc("GetSystemPowerStatus")

	ret, _, _ := getSystemPowerStatus.Call(uintptr(unsafe.Pointer(&status)))
	if ret == 0 {
		// If we can't get power status, default to desktop (more common)
		return false
	}

	// BatteryFlag values:
	// 1 = High (more than 66%)
	// 2 = Low (less than 33%)
	// 4 = Critical (less than 5%)
	// 8 = Charging
	// 128 = No battery
	// 255 = Unknown status

	// If BatteryFlag is 128 (no battery) or 255 (unknown), assume desktop
	if status.BatteryFlag == 128 || status.BatteryFlag == 255 {
		return false
	}

	// If we have a valid battery flag, it's likely a laptop
	return status.BatteryFlag != 0
}
