package wakey

import (
	"errors"
	"os"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	TgToken     string `koanf:"tg_token"`
	DBPath      string `koanf:"db_path"`
	Release     bool
	AdminID     int64 `koanf:"admin_id"`
	MaxJobs     int   `koand:"max_jobs"`
	MaxStateAge int   `koanf:"max_state_age"`
	Moderation  ModerationConfig
}

type ModerationConfig struct {
	LLM    LLMConfig `koanf:"llm"`
	Prompt string
	Temp   float64
	MaxTok int `koanf:"max_tok"`
}

type LLMConfig struct {
	Provider   LLMProvider
	APIKey     string `koanf:"api_key"`
	Model      string
	BaseURL    string `koanf:"base_url"`
	MaxRetries int    `koanf:"max_retries"`
	Timeout    int
}

func LoadConfig() (Config, error) {
	var kConf = koanf.New("/")

	var cfg Config

	path := os.Getenv("WAKEY_CONFIG")
	if path == "" {
		path = "wakey.toml"
	}

	err := kConf.Load(file.Provider(path), toml.Parser())
	if err != nil {
		return cfg, err
	}

	err = kConf.Unmarshal("", &cfg)
	if err != nil {
		return cfg, err
	}

	if cfg.TgToken == "" {
		return cfg, errors.New("telegram token is required")
	}

	return cfg, nil
}
