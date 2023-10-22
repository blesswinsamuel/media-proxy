package config

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/jessevdk/go-flags"
	"github.com/joho/godotenv"
)

// https://github.com/jessevdk/go-flags/issues/80#issuecomment-46948148
type Boolean struct{ Value bool }

func (b *Boolean) UnmarshalFlag(value string) error {
	bl, err := strconv.ParseBool(value)
	*b = Boolean{bl}
	return err
}

func (b Boolean) MarshalFlag() string {
	return strconv.FormatBool(b.Value)
}

// Config holds the runtime application config
type Config struct {
	Env string `long:"env" env:"GO_ENV" default:"development"`

	LogLevel string `long:"log-level" env:"LOG_LEVEL" default:"info" choice:"trace" choice:"debug" choice:"info" choice:"warn" choice:"error" choice:"fatal" choice:"panic" description:"Log level"`

	Config      func(s string) error `long:"config" env:"CONFIG" description:"Path to config file" json:"-"`
	Host        string               `long:"host" env:"HOST" default:"localhost" description:"Host to listen on"`
	Port        string               `long:"port" env:"PORT" default:"8080" description:"Port to listen on"`
	MetricsPort string               `long:"metrics-port" env:"METRICS_PORT" default:"8081" description:"Metrics port to listen on"`

	BaseURL           string  `long:"base-url" env:"BASE_URL" default:"" description:"Base URL"`
	EnableLoaderCache Boolean `long:"enable-loader-cache" env:"ENABLE_LOADER_CACHE" default:"true" description:"Enable loader cache"`
	EnableResultCache Boolean `long:"enable-result-cache" env:"ENABLE_RESULT_CACHE" default:"true" description:"Enable result cache"`
	CacheDir          string  `long:"cache-dir" env:"CACHE_DIR" default:"/tmp/cache" description:"Cache directory"`
	EnableUnsafe      Boolean `long:"enable-unsafe" env:"ENABLE_UNSAFE" default:"false" description:"Enable unsafe operations"`
	Secret            string  `long:"secret" env:"SECRET" default:"" description:"Secret"`

	Concurrency int `long:"concurrency" env:"CONCURRENCY" default:"8" description:"Concurrency"`
}

// ParseConfig parses and validates provided configuration into a config object
func ParseConfig(args []string) (*Config, error) {
	if args == nil {
		args = os.Args[1:]
	}
	var config = new(Config)
	config.Env = os.Getenv("GO_ENV")
	if config.Env == "" {
		config.Env = "development"
	}

	_ = godotenv.Load(".env." + config.Env + ".local")
	if config.Env != "test" {
		_ = godotenv.Load(".env.local")
	}
	_ = godotenv.Load(".env." + config.Env)
	_ = godotenv.Load() // The Original .env

	c := &Config{}

	err := c.parseFlags(args)
	if err != nil {
		return c, err
	}

	c.BaseURL = strings.TrimSuffix(c.BaseURL, "/")
	if c.BaseURL != "" {
		c.BaseURL = c.BaseURL + "/"
	}
	if !c.EnableUnsafe.Value {
		if c.Secret == "" {
			log.Fatal().Msg("SECRET must be set when ENABLE_UNSAFE=false")
		}
	}

	return c, nil
}

func (c *Config) parseFlags(args []string) error {
	p := flags.NewParser(c, flags.Default)

	i := flags.NewIniParser(p)
	c.Config = func(s string) error {
		return i.ParseFile(s)
	}

	_, err := p.ParseArgs(args)
	if err != nil {
		return handleFlagError(err)
	}

	return nil
}

func handleFlagError(err error) error {
	flagsErr, ok := err.(*flags.Error)
	if ok && flagsErr.Type == flags.ErrHelp {
		// Library has just printed cli help
		os.Exit(0)
	}

	return err
}

// Validate validates a config object
func (c *Config) Validate() {
	// Check for show stopper errors
}

func (c Config) String() string {
	jsonConf, _ := json.Marshal(c)
	return string(jsonConf)
}
