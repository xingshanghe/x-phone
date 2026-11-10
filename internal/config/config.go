package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr           string
	Driver         string
	DSN            string
	JWTSecret      string
	AdminUser      string
	AdminPassword  string
	ProfilesDir    string
	DataDir        string
	RedisAddr      string
	RedisPassword  string
	RedisDB        int
	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
	MinioBucket    string
	MinioSSL       bool
}

func Load() Config {
	dataDir := getenv("XPHONE_DATA_DIR", "data")
	return Config{
		Addr:           getenv("XPHONE_HTTP_ADDR", ":18110"),
		Driver:         getenv("XPHONE_DB_DRIVER", "sqlite"),
		DSN:            getenv("XPHONE_DSN", dataDir+"/xphone.db"),
		JWTSecret:      getenv("XPHONE_JWT_SECRET", "dev-only-change-me"),
		AdminUser:      getenv("XPHONE_ADMIN_USER", "admin"),
		AdminPassword:  os.Getenv("XPHONE_ADMIN_PASSWORD"),
		ProfilesDir:    getenv("XPHONE_PROFILES_DIR", "profiles"),
		DataDir:        dataDir,
		RedisAddr:      os.Getenv("XPHONE_REDIS_ADDR"),
		RedisPassword:  os.Getenv("XPHONE_REDIS_PASSWORD"),
		RedisDB:        redisDB(),
		MinioEndpoint:  os.Getenv("XPHONE_MINIO_ENDPOINT"),
		MinioAccessKey: os.Getenv("XPHONE_MINIO_ACCESS_KEY"),
		MinioSecretKey: os.Getenv("XPHONE_MINIO_SECRET_KEY"),
		MinioBucket:    getenv("XPHONE_MINIO_BUCKET", "xphone"),
		MinioSSL:       getenv("XPHONE_MINIO_SSL", "false") == "true",
	}
}

func redisDB() int {
	value := strings.TrimSpace(os.Getenv("XPHONE_REDIS_DB"))
	if value == "" {
		return 1
	}
	return AtoiDefault(value, 1)
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func AtoiDefault(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
