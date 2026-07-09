package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"keydeck.local/feasibilitylab/internal/fakeprovider"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18787", "listen address")
	flag.Parse()
	plan := fakeprovider.NewPlan()
	fmt.Printf("KeyDeck fake provider listening on http://%s\n", *addr)
	log.Fatal(http.ListenAndServe(*addr, fakeprovider.Handler(plan)))
}
