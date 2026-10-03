package extprovider

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"

	"ds2api/internal/config"
	"ds2api/internal/util"
)

var (
	storeMu sync.Mutex
)

func loadAllUnlocked() ([]Provider, error) {
	path := config.ExternalProvidersPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Provider{}, nil
		}
		return nil, err
	}

	var list []Provider
	if err := json.Unmarshal(data, &list); err != nil {
		return []Provider{}, nil
	}
	return list, nil
}

func saveAllUnlocked(list []Provider) error {
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return util.WriteFileAtomic(config.ExternalProvidersPath(), data)
}

func ListProviders() ([]Provider, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	return loadAllUnlocked()
}

func GetProvider(id string) (*Provider, error) {
	storeMu.Lock()
	defer storeMu.Unlock()

	list, err := loadAllUnlocked()
	if err != nil {
		return nil, err
	}
	for _, p := range list {
		if p.ID == id {
			cp := p
			return &cp, nil
		}
	}
	return nil, errors.New("provider not found")
}

func SaveProvider(p Provider) error {
	storeMu.Lock()
	defer storeMu.Unlock()

	list, err := loadAllUnlocked()
	if err != nil {
		return err
	}

	found := false
	for i, existing := range list {
		if existing.ID == p.ID {
			if p.Token == "" && existing.Token != "" {
				p.Token = existing.Token
			}
			list[i] = p
			found = true
			break
		}
	}
	if !found {
		if p.ID == "" {
			p.ID = GenerateProviderID()
		}
		if p.ConnectionStatus == "" {
			p.ConnectionStatus = "unknown"
		}
		list = append(list, p)
	}

	return saveAllUnlocked(list)
}

func DeleteProvider(id string) error {
	storeMu.Lock()
	defer storeMu.Unlock()

	list, err := loadAllUnlocked()
	if err != nil {
		return err
	}

	next := make([]Provider, 0, len(list))
	found := false
	for _, p := range list {
		if p.ID == id {
			found = true
		} else {
			next = append(next, p)
		}
	}
	if !found {
		return errors.New("provider not found")
	}

	return saveAllUnlocked(next)
}

func UpdateProviderModels(id string, models []ProviderModel, connStatus string) error {
	storeMu.Lock()
	defer storeMu.Unlock()

	list, err := loadAllUnlocked()
	if err != nil {
		return err
	}

	for i, p := range list {
		if p.ID == id {
			list[i].Models = models
			list[i].ConnectionStatus = connStatus
			list[i].LastSyncAt = time.Now().Unix()
			return saveAllUnlocked(list)
		}
	}
	return errors.New("provider not found")
}

func AppendInspectionLog(id string, log InspectionLog) error {
	storeMu.Lock()
	defer storeMu.Unlock()

	list, err := loadAllUnlocked()
	if err != nil {
		return err
	}

	for i, p := range list {
		if p.ID == id {
			// Prepend newest log, cap at 50 logs
			list[i].InspectionHistory = append([]InspectionLog{log}, list[i].InspectionHistory...)
			if len(list[i].InspectionHistory) > 50 {
				list[i].InspectionHistory = list[i].InspectionHistory[:50]
			}
			return saveAllUnlocked(list)
		}
	}
	return errors.New("provider not found")
}
