package config

import (
	"fmt"
	"os"
)

type Config struct {
	DatabaseURL string
	ServerPort  string
	AuthToken   string
}

func Load() (*Config, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "8080"
	}

	authToken := os.Getenv("AUTH_TOKEN")
	if authToken == "" {
		return nil, fmt.Errorf("AUTH_TOKEN is required")
	}

	return &Config{
		DatabaseURL: dbURL,
		ServerPort:  port,
		AuthToken:   authToken,
	}, nil
}
