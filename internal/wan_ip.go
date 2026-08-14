package internal

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

var (
	// wanIPURL is the service queried for the host's WAN IP. It is a package
	// var so tests can point it at an httptest server.
	wanIPURL = "https://ipv4.icanhazip.com"

	// wanIPClient performs the WAN IP lookup with a bounded timeout so a
	// hung remote service cannot stall the check loop indefinitely.
	wanIPClient = &http.Client{Timeout: 10 * time.Second}
)

func GetWANIP() (string, error) {
	log.Debug().Msg("Checking current WAN IP")
	res, err := wanIPClient.Get(wanIPURL)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("WAN IP lookup returned unexpected status %d", res.StatusCode)
	}

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(resBody)), nil
}

// WatchWanIP reconciles the zone against the host's WAN IP every interval,
// until ctx is cancelled. The first check is the caller's to make: this loop
// waits out one interval before doing anything.
//
// The wait is a ticker rather than a sleep so that cancellation is noticed at
// once. A sleeping goroutine cannot be selected on, and would sit through the
// remainder of the interval before returning.
func (r *Reconciler) WatchWanIP(ctx context.Context, interval time.Duration) {
	log.Debug().Msg("Starting WAN IP check routine")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Debug().Msg("Stopping WAN IP check routine")
			return
		case <-ticker.C:
		}

		wanIP, err := GetWANIP()
		if err != nil {
			log.Error().Err(err).Msg("")
			continue
		}

		log.Info().Str("WAN_IP", wanIP).Msg("WAN IP check")
		if err := r.CompareStateToWanIP(ctx, wanIP); err != nil {
			log.Error().Err(err).Msg("")
		}
	}
}

func (r *Reconciler) InitialWanIPCheck(ctx context.Context) error {
	log.Debug().Msg("Performing initial WAN IP check")
	wanIP, err := GetWANIP()
	if err != nil {
		return err
	}

	return r.CompareStateToWanIP(ctx, wanIP)
}
