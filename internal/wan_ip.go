package internal

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

func GetWANIP() string {
	log.Debug().Msg("Checking current WAN IP")
	res, err := http.Get("https://ipv4.icanhazip.com")

	if err != nil {
		log.Error().Err(err).Msg("Failed to fetch WAN IP")
		return ""
	}
	defer res.Body.Close()

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		log.Error().Err(err).Msg("Failed to read WAN IP response")
		return ""
	}

	return strings.TrimSpace(string(resBody))
}

func WanIPCheck(checkInterval int) {
	log.Debug().Msg("Starting WAN IP check routine")
	for {
		time.Sleep(time.Duration(checkInterval) * time.Second)
		log.Info().Str("WAN_IP", GetWANIP()).Msg("WAN IP check")
		if err := CompareStateToWanIP(GetWANIP()); err != nil {
			log.Error().Err(err).Msg("")
		}
	}
}

func InitialWanIPCheck() error {
	log.Debug().Msg("Performing initial WAN IP check")
	err := CompareStateToWanIP(GetWANIP())
	if err != nil {
		return err
	}

	return nil
}
