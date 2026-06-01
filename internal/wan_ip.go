package internal

import (
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

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(resBody)), nil
}

func WanIPCheck(checkInterval int) {
	log.Debug().Msg("Starting WAN IP check routine")
	for {
		time.Sleep(time.Duration(checkInterval) * time.Second)

		wanIP, err := GetWANIP()
		if err != nil {
			log.Error().Err(err).Msg("")
			continue
		}

		log.Info().Str("WAN_IP", wanIP).Msg("WAN IP check")
		if err := CompareStateToWanIP(wanIP); err != nil {
			log.Error().Err(err).Msg("")
		}
	}
}

func InitialWanIPCheck() error {
	log.Debug().Msg("Performing initial WAN IP check")
	wanIP, err := GetWANIP()
	if err != nil {
		return err
	}

	return CompareStateToWanIP(wanIP)
}
