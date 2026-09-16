package main

import (
	"log"
	"net/http"

	"github.com/knowe666/go-musthave-diploma-tpl/internal/accrual"
	"github.com/knowe666/go-musthave-diploma-tpl/internal/api"
	"github.com/knowe666/go-musthave-diploma-tpl/internal/auth"
	"github.com/knowe666/go-musthave-diploma-tpl/internal/config"
	"github.com/knowe666/go-musthave-diploma-tpl/internal/storage"
)

func main() {
	cfg := config.Load()
	if cfg.DatabaseURI == "" {
		log.Fatal("DATABASE_URI must be set")
	}
	if cfg.AccrualAddress == "" {
		log.Fatal("ACCRUAL_SYSTEM_ADDRESS must be set")
	}
	if cfg.AuthSecret == "" {
		log.Fatal("AUTH_SECRET must be set")
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

	service := api.New(store, auth.New(cfg.AuthSecret), accrual.New(cfg.AccrualAddress))
	go service.SyncPendingOrders()

	log.Printf("starting gophermart on %s", cfg.Address)
	if err := http.ListenAndServe(cfg.Address, service.Router()); err != nil {
		log.Fatalf("start HTTP server: %v", err)
	}
}
