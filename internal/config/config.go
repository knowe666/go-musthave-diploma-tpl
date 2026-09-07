package config

import (
	"flag"
	"os"
	"strings"
)

// Config хранит параметры среды выполнения сервиса.
type Config struct {
	Address        string
	DatabaseURI    string
	AccrualAddress string
}

// Load читает параметры из переменных окружения и флагов командной строки.
func Load() Config {
	cfg := Config{
		Address:        getenv("RUN_ADDRESS", "localhost:8080"),
		DatabaseURI:    getenv("DATABASE_URI", ""),
		AccrualAddress: getenv("ACCRUAL_SYSTEM_ADDRESS", ""),
	}

	flag.StringVar(&cfg.Address, "a", cfg.Address, "address to bind the HTTP server to")
	flag.StringVar(&cfg.DatabaseURI, "d", cfg.DatabaseURI, "database connection URI")
	flag.StringVar(&cfg.AccrualAddress, "r", cfg.AccrualAddress, "accrual system base address")
	flag.Parse()

	if cfg.AccrualAddress != "" && !strings.Contains(cfg.AccrualAddress, "://") {
		cfg.AccrualAddress = "http://" + cfg.AccrualAddress
	}
	cfg.AccrualAddress = strings.TrimRight(cfg.AccrualAddress, "/")

	return cfg
}

func getenv(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
