// Package pluginmeta reads the immutable command metadata compiled with a plugin.
package pluginmeta

import (
	"encoding/json"
	"errors"
)

type Command struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Usage       string `json:"usage"`
	Permission  string `json:"permission"`
	Trigger     struct {
		Names []string `json:"names"`
	} `json:"trigger"`
}
type Group struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Commands []string `json:"commands"`
}
type Manifest struct {
	Services []struct {
		Name    string   `json:"name"`
		Version int      `json:"version"`
		Methods []string `json:"methods"`
	} `json:"services"`
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Version        string    `json:"version"`
	License        string    `json:"license"`
	MinCoreVersion string    `json:"min_core_version"`
	Commands       []Command `json:"commands"`
	Groups         []Group   `json:"command_groups"`
}

func Read(raw []byte) (Manifest, error) {
	var m Manifest
	if len(raw) > 1024*1024 || json.Unmarshal(raw, &m) != nil || m.ID == "" || m.Version == "" || len(m.Commands) > 2048 {
		return m, errors.New("invalid plugin help metadata")
	}
	return m, nil
}
