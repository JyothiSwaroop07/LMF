package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config holds all configuration for an LMF service
type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	GRPC      GRPCConfig      `mapstructure:"grpc"`
	Redis     RedisConfig     `mapstructure:"redis"`
	Kafka     KafkaConfig     `mapstructure:"kafka"`
	NRF       NRFConfig       `mapstructure:"nrf"`
	AMF       AMFConfig       `mapstructure:"amf"`
	UDM       UDMConfig       `mapstructure:"udm"`
	GNSS      GNSSConfig      `mapstructure:"gnss"`
	Tracing   TracingConfig   `mapstructure:"tracing"`
	Metrics   MetricsConfig   `mapstructure:"metrics"`
	Log       LogConfig       `mapstructure:"log"`
	Cassandra CassandraConfig `mapstructure:"cassandra"`
	Services  ServicesConfig  `mapstructure:"services"`
}

// GetCassandraHosts returns Cassandra host addresses.
func (c *Config) GetCassandraHosts() []string {
	if len(c.Cassandra.Hosts) > 0 {
		return c.Cassandra.Hosts
	}
	return []string{"localhost:9042"}
}

// ServerConfig holds HTTP server settings
type ServerConfig struct {
	Host    string `mapstructure:"host"`
	Port    int    `mapstructure:"port"`
	TLSCert string `mapstructure:"tls_cert"`
	TLSKey  string `mapstructure:"tls_key"`
}

// GRPCConfig holds gRPC server and client settings
type GRPCConfig struct {
	Port                 int    `mapstructure:"port"`
	Addr                 string `mapstructure:"addr"` // overrides Port when set
	MaxConcurrentStreams uint32 `mapstructure:"max_concurrent_streams"`
	MaxRecvMsgSize       int    `mapstructure:"maxRecvMsgSize"`
}

// ListenAddr returns the gRPC listen address, preferring Addr over Port.
func (g *GRPCConfig) ListenAddr() string {
	if g.Addr != "" {
		return g.Addr
	}
	return fmt.Sprintf(":%d", g.Port)
}

// RedisConfig holds Redis cluster configuration
type RedisConfig struct {
	Addresses  []string `mapstructure:"addresses"`
	Password   string   `mapstructure:"password"`
	DB         int      `mapstructure:"db"`
	MaxRetries int      `mapstructure:"max_retries"`
	PoolSize   int      `mapstructure:"pool_size"`
}

// KafkaConfig holds Kafka configuration
type KafkaConfig struct {
	Brokers       []string `mapstructure:"brokers"`
	ConsumerGroup string   `mapstructure:"consumer_group"`
	TopicEvents   string   `mapstructure:"topic_events"`
	TopicSubs     string   `mapstructure:"topic_subscriptions"`
}

// NRFConfig holds NRF connection settings
type NRFConfig struct {
	BaseURL      string `mapstructure:"base_url"`
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
	TokenURL     string `mapstructure:"token_url"`
}

// AMFConfig holds AMF connection settings
type AMFConfig struct {
	BaseURL string `mapstructure:"base_url"`
}

// UDMConfig holds UDM connection settings
type UDMConfig struct {
	BaseURL             string `mapstructure:"base_url"`
	TimeoutSeconds      int    `mapstructure:"timeout_seconds"`
	DefaultPrivacyClass string `mapstructure:"default_privacy_class"`
}

// GNSSConfig holds GNSS reference server settings
type GNSSConfig struct {
	ServerURL              string `mapstructure:"server_url"`
	RefreshIntervalSeconds int    `mapstructure:"refresh_interval_seconds"`
}

// TracingConfig holds distributed tracing settings
type TracingConfig struct {
	JaegerEndpoint string  `mapstructure:"jaeger_endpoint"`
	ServiceName    string  `mapstructure:"service_name"`
	SamplingRate   float64 `mapstructure:"sampling_rate"`
}

// MetricsConfig holds Prometheus metrics settings
type MetricsConfig struct {
	Port int    `mapstructure:"port"`
	Path string `mapstructure:"path"`
}

// LogConfig holds logging settings
type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// CassandraConfig holds Cassandra connection settings
type CassandraConfig struct {
	Hosts    []string `mapstructure:"hosts"`
	Keyspace string   `mapstructure:"keyspace"`
	Username string   `mapstructure:"username"`
	Password string   `mapstructure:"password"`
}

// ServicesConfig holds addresses of downstream LMF microservices
type ServicesConfig struct {
	LocationRequest string `mapstructure:"locationRequest"`
	SessionManager  string `mapstructure:"sessionManager"`
	MethodSelector  string `mapstructure:"methodSelector"`
	ProtocolHandler string `mapstructure:"protocolHandler"`
	GnssEngine      string `mapstructure:"gnssEngine"`
	TdoaEngine      string `mapstructure:"tdoaEngine"`
	EcidEngine      string `mapstructure:"ecidEngine"`
	RttEngine       string `mapstructure:"rttEngine"`
	FusionEngine    string `mapstructure:"fusionEngine"`
	QosManager      string `mapstructure:"qosManager"`
	PrivacyAuth     string `mapstructure:"privacyAuth"`
	AssistanceData  string `mapstructure:"assistanceData"`
	EventManager    string `mapstructure:"eventManager"`
}

// Load reads configuration from file and environment variables
func Load(path string) (*Config, error) {
	v := viper.New()

	if path != "" {
		v.SetConfigFile(path)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath("./config")
		v.AddConfigPath(".")
		v.AddConfigPath("/etc/lmf")
	}

	// Allow env var overrides: LMF_GRPC_PORT → grpc.port
	v.SetEnvPrefix("LMF")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Set defaults
	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}

	return cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)
	v.SetDefault("grpc.port", 9090)
	v.SetDefault("grpc.max_concurrent_streams", 1000)
	v.SetDefault("redis.addresses", []string{"localhost:6379"})
	v.SetDefault("redis.db", 0)
	v.SetDefault("redis.max_retries", 3)
	v.SetDefault("redis.pool_size", 20)
	v.SetDefault("kafka.brokers", []string{"localhost:9092"})
	v.SetDefault("kafka.consumer_group", "lmf-default")
	v.SetDefault("kafka.topic_events", "lmf.location.events")
	v.SetDefault("kafka.topic_subscriptions", "lmf.subscriptions")
	v.SetDefault("gnss.refresh_interval_seconds", 30)
	v.SetDefault("tracing.sampling_rate", 0.1)
	v.SetDefault("metrics.port", 9090)
	v.SetDefault("metrics.path", "/metrics")
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
	v.SetDefault("udm.timeout_seconds", 2)
	v.SetDefault("udm.default_privacy_class", "CLASS_B")
}
