// Package hiveconf reads the subset of hive-site.xml needed for connections.
package hiveconf

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type configuration struct {
	Properties []property `xml:"property"`
}

type property struct {
	Name  string `xml:"name"`
	Value string `xml:"value"`
}

// Load reads hive-site.xml from dir (dir may name the file directly). A
// missing file returns an empty map and no error.
func Load(dir string) (map[string]string, error) {
	path := dir
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		path = filepath.Join(dir, "hive-site.xml")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var conf configuration
	if err := xml.Unmarshal(data, &conf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := make(map[string]string, len(conf.Properties))
	for _, p := range conf.Properties {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		out[name] = strings.TrimSpace(p.Value)
	}
	return out, nil
}

// DefaultDir returns the conventional Hive configuration directory, or "".
func DefaultDir() string {
	if dir := os.Getenv("HIVE_CONF_DIR"); dir != "" {
		return dir
	}
	if home := os.Getenv("HIVE_HOME"); home != "" {
		return filepath.Join(home, "conf")
	}
	return ""
}
