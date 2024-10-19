package wakey

import (
	"errors"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	TgToken string `koanf:"tg_token"`
	DBPath  string `koanf:"db_path"`
	Release bool
	AdminID int64 `koanf:"admin_id"`
}

func LoadConfig() (Config, error) {
	var kConf = koanf.New("/")

	var cfg Config

	err := kConf.Load(file.Provider("wakey.toml"), toml.Parser())
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
