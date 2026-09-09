package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	configPath := flag.String("config-path", "/etc/kiwivm/config.yml", "path to config file")
	listen := flag.String("listen", ":9103", "listen address")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	prometheus.MustRegister(NewCollector(cfg))

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	log.Printf("listening on %s, %d node(s) configured", *listen, len(cfg.Nodes))
	log.Fatal(http.ListenAndServe(*listen, mux))
}
