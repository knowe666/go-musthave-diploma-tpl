package main

import (
	"log"
	"net/http"

	"github.com/knowe666/internal/accrual"
	"github.com/knowe666/internal/api"
	"github.com/knowe666/internal/auth"
	"github.com/knowe666/internal/config"
	"github.com/knowe666/internal/storage"
)

func main() {
	cfg := config.Load()
	if cfg.DatabaseURI == "" {
		log.Fatal("DATABASE_URI must be set")
	}
	if cfg.AccrualAddress == "" {
		log.Fatal("ACCRUAL_SYSTEM_ADDRESS must be set")
	}

	db, err := storage.Open(cfg.DatabaseURI)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	store := storage.New(db)
	if err := store.Init(); err != nil {
		log.Fatalf("init database: %v", err)
	}

	service := api.New(store, auth.New("gophermart-auth-secret"), accrual.New(cfg.AccrualAddress))
	go service.SyncPendingOrders()

	log.Printf("starting gophermart on %s", cfg.Address)
	if err := http.ListenAndServe(cfg.Address, service.Router()); err != nil {
		log.Fatalf("start HTTP server: %v", err)
	}
}
