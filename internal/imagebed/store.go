package imagebed

import (
	"encoding/json"
	"errors"
	"os"
	"sync"

	"ds2api/internal/config"
	"ds2api/internal/util"
)

var (
	storeMu sync.Mutex
)

func LoadConfig() (Config, error) {
	storeMu.Lock()
	defer storeMu.Unlock()

	path := config.ImageBedPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func SaveConfig(cfg Config) error {
	storeMu.Lock()
	defer storeMu.Unlock()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return util.WriteFileAtomic(config.ImageBedPath(), data)
}

func ClearConfig() error {
	storeMu.Lock()
	defer storeMu.Unlock()

	path := config.ImageBedPath()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func LoadHistory() ([]HistoryItem, error) {
	storeMu.Lock()
	defer storeMu.Unlock()

	path := config.ImageBedHistoryPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []HistoryItem{}, nil
		}
		return nil, err
	}

	var items []HistoryItem
	if err := json.Unmarshal(data, &items); err != nil {
		return []HistoryItem{}, nil
	}
	return items, nil
}

func SaveHistory(items []HistoryItem) error {
	if len(items) > 100 {
		items = items[len(items)-100:]
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return util.WriteFileAtomic(config.ImageBedHistoryPath(), data)
}

func AddHistoryItem(item HistoryItem) error {
	storeMu.Lock()
	defer storeMu.Unlock()

	items, _ := loadHistoryUnlocked()
	// Prepend so newest is first
	items = append([]HistoryItem{item}, items...)
	if len(items) > 100 {
		items = items[:100]
	}

	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return util.WriteFileAtomic(config.ImageBedHistoryPath(), data)
}

func DeleteHistoryItem(id string) (*HistoryItem, error) {
	storeMu.Lock()
	defer storeMu.Unlock()

	items, _ := loadHistoryUnlocked()
	var removed *HistoryItem
	var next []HistoryItem
	for _, it := range items {
		if it.ID == id {
			copy := it
			removed = &copy
		} else {
			next = append(next, it)
		}
	}
	if removed == nil {
		return nil, errors.New("item not found")
	}

	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := util.WriteFileAtomic(config.ImageBedHistoryPath(), data); err != nil {
		return nil, err
	}
	return removed, nil
}

func ClearHistory() error {
	storeMu.Lock()
	defer storeMu.Unlock()

	path := config.ImageBedHistoryPath()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func loadHistoryUnlocked() ([]HistoryItem, error) {
	path := config.ImageBedHistoryPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return []HistoryItem{}, nil
	}
	var items []HistoryItem
	if err := json.Unmarshal(data, &items); err != nil {
		return []HistoryItem{}, nil
	}
	return items, nil
}
