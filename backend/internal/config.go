package internal

import (
	"os"
	"strconv"
)

type Config struct {
	PushPort        string
	APIPort         string
	DatabaseURL     string
	RedisURL        string
	MinIOEndpoint   string
	MinIOAccessKey  string
	MinIOSecretKey  string
	MinIOBucket     string
	MinIOPublicBase string
	NoAuthMode      bool
	DefaultPassword string
	CommandInterval int
	LogLevel        string
	EventCallbackIP string // IP the device should push events to (this server's reachable IP)
	PublicPushURL   string // Host:port the device should be configured with for OTAP/ISUP (e.g. push.face_auth.qbot.now)
	PublicPushHost  string // Public host alone (for ISUP form which only takes IP/host)
	TLSPort         string // If set, also listen on this port with TLS (self-signed)
	CertDir         string // Where to persist the self-signed cert
}

func LoadConfig() Config {
	return Config{
		PushPort:        getenv("PUSH_PORT", "7660"),
		APIPort:         getenv("API_PORT", "8080"),
		DatabaseURL:     getenv("DATABASE_URL", "postgres://hik:hikpush@localhost:5432/hikpush?sslmode=disable"),
		RedisURL:        getenv("REDIS_URL", "localhost:6379"),
		MinIOEndpoint:   getenv("MINIO_ENDPOINT", "localhost:9000"),
		MinIOAccessKey:  getenv("MINIO_ACCESS_KEY", "minioadmin"),
		MinIOSecretKey:  getenv("MINIO_SECRET_KEY", "minioadmin123"),
		MinIOBucket:     getenv("MINIO_BUCKET", "hikpush"),
		MinIOPublicBase: getenv("MINIO_PUBLIC_BASE", ""),
		NoAuthMode:      getenv("NO_AUTH_MODE", "true") == "true",
		DefaultPassword: getenv("DEVICE_DEFAULT_PASSWORD", ""),
		CommandInterval: atoiDefault(getenv("COMMAND_INTERVAL", "5"), 5),
		LogLevel:        getenv("LOG_LEVEL", "info"),
		EventCallbackIP: getenv("EVENT_CALLBACK_IP", ""),
		PublicPushURL:   getenv("PUBLIC_PUSH_URL", ""),
		PublicPushHost:  getenv("PUBLIC_PUSH_HOST", ""),
		TLSPort:         getenv("TLS_PORT", ""),
		CertDir:         getenv("CERT_DIR", "/app/certs"),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func atoiDefault(s string, def int) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}
