package merchants

import (
	"os"
	"strings"
	"sync/atomic"
)

type ConfigManager struct {
	mode atomic.Value
}

func NewConfigManager() *ConfigManager {
	cm := &ConfigManager{}
	cm.ReloadFromEnv()
	return cm
}

func (cm *ConfigManager) GetAuthMode() AuthMode {
	v := cm.mode.Load()
	if v == nil {
		return AuthModeShadow
	}
	return v.(AuthMode)
}

func (cm *ConfigManager) SetAuthMode(mode AuthMode) {
	cm.mode.Store(mode)
}

func (cm *ConfigManager) ReloadFromEnv() {
	envMode := strings.ToLower(os.Getenv("MERCHANT_AUTH_MODE"))
	switch envMode {
	case "legacy":
		cm.SetAuthMode(AuthModeLegacy)
	case "canonical":
		cm.SetAuthMode(AuthModeCanonical)
	default:
		cm.SetAuthMode(AuthModeShadow)
	}
}
