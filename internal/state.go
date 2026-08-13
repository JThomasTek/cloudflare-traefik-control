package internal

import (
	"os"
	"sync"

	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

var (
	stateFolder = "/etc/ctc/"
	stateFile   = stateFolder + "state.yml"
	mu          sync.Mutex
)

type state struct {
	WanIP   string
	Routers map[string]Router
}

func generateState() error {
	log.Debug().Msg("Generate the state file")

	// First check if directory exists and if not then create it
	if _, err := os.Stat(stateFolder); os.IsNotExist(err) {
		err = os.Mkdir(stateFolder, 0700)
		if err != nil {
			return err
		}
	}

	// Create the state file
	file, err := os.Create(stateFile)
	if err != nil {
		return err
	}

	defer file.Close()

	return nil
}

func getState() (state, error) {
	log.Debug().Msg("Reading state file data")

	// First check that state file exists
	_, err := os.Stat(stateFile)
	if err != nil {
		// If it doesn't exist, generate it
		err = generateState()

		if err != nil {
			return state{}, err
		}
	}

	var s state

	mu.Lock()
	buffer, err := os.ReadFile(stateFile)
	mu.Unlock()
	if err != nil {
		return s, err
	}

	err = yaml.Unmarshal(buffer, &s)
	if err != nil {
		return s, err
	}

	// Ensure the map is always usable so callers can assign to it without
	// risking a nil-map panic (e.g. on a freshly created/empty state file).
	if s.Routers == nil {
		s.Routers = make(map[string]Router)
	}

	return s, nil
}

func writeState(newState state) error {
	log.Debug().Msg("Writing to the state file")

	data, err := yaml.Marshal(newState)
	if err != nil {
		return err
	}

	mu.Lock()
	err = os.WriteFile(stateFile, data, 0600)
	mu.Unlock()
	if err != nil {
		return err
	}

	return nil
}
