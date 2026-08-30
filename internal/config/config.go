package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port          string
	DBPath        string
	JWTSecret     string
	AdminUsername string
	AdminPassword string
	BodyLimitMB   int64
}

func Load() *Config {
	return &Config{
		Port:          env("PORT", "8080"),
		DBPath:        env("DB_PATH", "data/tokenhub.db"),
		JWTSecret:     env("JWT_SECRET", ""), // 空则启动时生成并持久化到 settings 表
		AdminUsername: env("ADMIN_USERNAME", "admin"),
		AdminPassword: env("ADMIN_PASSWORD", "admin123"),
		BodyLimitMB:   int64(envInt("BODY_LIMIT_MB", 20)),
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
