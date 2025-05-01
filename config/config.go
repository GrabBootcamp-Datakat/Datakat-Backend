package config

import (
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

type Config struct {
	Server        ServerConfig
	Database      DatabaseConfig
	Kafka         KafkaConfig
	LogProcessor  LogProcessorConfig
	Elasticsearch ElasticsearchConfig
	TimescaleDB   TimescaleDBConfig
	FileState     FileStateConfig
	APIKey        string
}

type ServerConfig struct {
	Port string
}
type KafkaConfig struct {
	Brokers       []string
	LogTopic      string
	ConsumerGroup string
}

type LogProcessorConfig struct {
	LogDirectory string // Root directory containing application_* folders
	Schedule     string
	BatchSize    int
	MaxBatchWait time.Duration
}

type ElasticsearchConfig struct {
	Addresses     []string
	Username      string
	Password      string
	LogIndex      string
	BulkWorkers   int           // Number of concurrent goroutines for bulk indexing
	FlushBytes    int           // Flush threshold for bulk indexer
	FlushInterval time.Duration // Flush interval for bulk indexer
}

type TimescaleDBConfig struct {
	DSN string
}
type FileStateConfig struct {
	FilePath string
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
}

func NewConfig() (*Config, error) {
	// Configure Viper to read .env file
	viper.SetConfigName(".env")
	viper.SetConfigType("env")
	viper.AddConfigPath(".")

	// Enable automatic environment variable loading
	viper.AutomaticEnv()

	viper.SetDefault("SERVER_PORT", "8080")
	viper.SetDefault("KAFKA_BROKERS", "localhost:9092")
	viper.SetDefault("KAFKA_LOG_TOPIC", "log_entries")
	viper.SetDefault("KAFKA_CONSUMER_GROUP", "log_processor_group")
	viper.SetDefault("LOG_PROCESSOR_DIRECTORY", "./logs")
	viper.SetDefault("LOG_PROCESSOR_SCHEDULE", "*/300 * * * * *") // Every 300 seconds
	viper.SetDefault("LOG_PROCESSOR_BATCH_SIZE", 100)
	viper.SetDefault("LOG_PROCESSOR_MAX_BATCH_WAIT", "5s")
	viper.SetDefault("ELASTICSEARCH_ADDRESSES", "http://localhost:9200")
	viper.SetDefault("ELASTICSEARCH_LOG_INDEX", "applogs")
	viper.SetDefault("ELASTICSEARCH_BULK_WORKERS", 2)
	viper.SetDefault("ELASTICSEARCH_FLUSH_BYTES", 1048576) // 1MB
	viper.SetDefault("ELASTICSEARCH_FLUSH_INTERVAL", "5s")
	viper.SetDefault("TIMESCALEDB_DSN", "postgres://user:password@localhost:5432/logsdb?sslmode=disable")
	viper.SetDefault("FILE_STATE_PATH", "./log_state.json")

	// Read config file
	if err := viper.ReadInConfig(); err != nil {
		log.Warn().Err(err).Msg("Error reading config file")
	}

	var config Config
	config.Server.Port = viper.GetString("SERVER_PORT")

	config.Database.Host = viper.GetString("DATABASE_HOST")
	config.Database.Port = viper.GetString("DATABASE_PORT")
	config.Database.User = viper.GetString("DATABASE_USER")
	config.Database.Password = viper.GetString("DATABASE_PASSWORD")
	config.Database.Name = viper.GetString("DATABASE_NAME")

	// --- Kafka ---
	kafkaBrokers := viper.GetString("KAFKA_BROKERS")
	config.Kafka.Brokers = strings.Split(kafkaBrokers, ",")
	config.Kafka.LogTopic = viper.GetString("KAFKA_LOG_TOPIC")
	config.Kafka.ConsumerGroup = viper.GetString("KAFKA_CONSUMER_GROUP")

	// --- Log Processor ---
	config.LogProcessor.LogDirectory = viper.GetString("LOG_PROCESSOR_DIRECTORY")
	config.LogProcessor.Schedule = viper.GetString("LOG_PROCESSOR_SCHEDULE")
	config.LogProcessor.BatchSize = viper.GetInt("LOG_PROCESSOR_BATCH_SIZE")
	config.LogProcessor.MaxBatchWait = viper.GetDuration("LOG_PROCESSOR_MAX_BATCH_WAIT")

	// --- Elasticsearch ---
	esAddresses := viper.GetString("ELASTICSEARCH_ADDRESSES")
	config.Elasticsearch.Addresses = strings.Split(esAddresses, ",")
	config.Elasticsearch.LogIndex = viper.GetString("ELASTICSEARCH_LOG_INDEX")
	config.Elasticsearch.BulkWorkers = viper.GetInt("ELASTICSEARCH_BULK_WORKERS")
	config.Elasticsearch.FlushBytes = viper.GetInt("ELASTICSEARCH_FLUSH_BYTES")
	config.Elasticsearch.FlushInterval = viper.GetDuration("ELASTICSEARCH_FLUSH_INTERVAL")

	// --- TimescaleDB ---
	config.TimescaleDB.DSN = viper.GetString("TIMESCALEDB_DSN")

	// --- File State ---
	config.FileState.FilePath = viper.GetString("FILE_STATE_PATH")

	config.APIKey = viper.GetString("API_KEY")

	log.Info().Interface("config", config).Msg("Config loaded")
	return &config, nil
}
