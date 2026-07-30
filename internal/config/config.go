// Package config handles configuration loading from environment variables.
package config

import (
	"fmt"
	"time"

	env "github.com/caarlos0/env/v11"
)

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Address         string        `env:"ADDRESS"          envDefault:":8080"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"15s"`
	ReadTimeout     time.Duration `env:"READ_TIMEOUT"     envDefault:"15s"`
	WriteTimeout    time.Duration `env:"WRITE_TIMEOUT"    envDefault:"15s"`
	IdleTimeout     time.Duration `env:"IDLE_TIMEOUT"     envDefault:"60s"`
	RequestTimeout  time.Duration `env:"REQUEST_TIMEOUT"  envDefault:"30s"`
}

// ProviderConfig holds service provider identity and metadata.
type ProviderConfig struct {
	Name        string `env:"NAME,notEmpty"`
	DisplayName string `env:"DISPLAY_NAME"`
	Endpoint    string `env:"ENDPOINT,notEmpty"`
	Region      string `env:"REGION"`
	Zone        string `env:"ZONE"`
}

// DCMConfig holds DCM registry connection settings.
type DCMConfig struct {
	RegistrationURL string `env:"REGISTRATION_URL,notEmpty"`
}

// KubernetesConfig holds Kubernetes-specific settings.
type KubernetesConfig struct {
	Namespace  string `env:"NAMESPACE"   envDefault:"default"`
	Kubeconfig string `env:"KUBECONFIG"`
}

// NATSConfig holds NATS connection settings (required when monitoring is enabled).
type NATSConfig struct {
	URL string `env:"URL"`
}

// MonitoringConfig holds status monitoring settings.
type MonitoringConfig struct {
	DebounceMs   int           `env:"DEBOUNCE_MS"   envDefault:"500"`
	ResyncPeriod time.Duration `env:"RESYNC_PERIOD" envDefault:"10m"`
}

// Config is the root configuration for the service provider.
type Config struct {
	Server     ServerConfig     `envPrefix:"SP_SERVER_"`
	Provider   ProviderConfig   `envPrefix:"SP_"`
	DCM        DCMConfig        `envPrefix:"DCM_"`
	Kubernetes KubernetesConfig `envPrefix:"SP_K8S_"`
	NATS       NATSConfig       `envPrefix:"SP_NATS_"`
	Monitoring MonitoringConfig `envPrefix:"SP_MONITOR_"`
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("loading configuration: %w", err)
	}
	return cfg, nil
}
