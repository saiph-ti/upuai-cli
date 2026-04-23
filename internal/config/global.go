package config

import (
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

const (
	DefaultAPIURL      = "https://api.upuai.com.br"
	DefaultWebURL      = "https://app.upuai.com.br"
	DefaultEnvironment = "staging"
	globalConfigFile   = "config"
)

func InitGlobalConfig() {
	home, _ := os.UserHomeDir()
	configDir := filepath.Join(home, upuaiDir)

	viper.SetConfigName(globalConfigFile)
	viper.SetConfigType("json")
	viper.AddConfigPath(configDir)

	viper.SetDefault("apiUrl", DefaultAPIURL)
	viper.SetDefault("webUrl", DefaultWebURL)
	viper.SetDefault("defaultEnvironment", DefaultEnvironment)
	viper.SetDefault("output", "table")

	viper.SetEnvPrefix("UPUAI")
	viper.AutomaticEnv()

	_ = viper.ReadInConfig()
}

func GetAPIURL() string {
	if url := os.Getenv("UPUAI_API_URL"); url != "" {
		return url
	}
	return viper.GetString("apiUrl")
}

func GetDefaultEnvironment() string {
	return viper.GetString("defaultEnvironment")
}

func GetWebURL() string {
	if url := os.Getenv("UPUAI_WEB_URL"); url != "" {
		return url
	}
	return viper.GetString("webUrl")
}

func GetDefaultOutput() string {
	return viper.GetString("output")
}
