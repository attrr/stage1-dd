package markers

import (
	"os"
	"path/filepath"
	"strings"
)

type Markers struct {
	rootPrefix string
	markersDir string
}

func New(rootPrefix string, dir string) *Markers {
	markers := Markers{rootPrefix: rootPrefix}
	markers.resolveMarkerDir(dir)
	return &markers
}

func (m *Markers) resolveMarkerDir(dir string) {
	// ignore rootPrefix if env set explictly
	if env := os.Getenv("FAILOVER_MARKER_DIR"); env != "" {
		m.markersDir = env
		return
	}
	if env := os.Getenv("FAILOVER_STATE_DIR"); env != "" {
		m.markersDir = env
		return
	}
	m.markersDir = filepath.Join(m.rootPrefix, dir)
}

func (m *Markers) CreateMarker(name string) error {
	if err := os.MkdirAll(m.markersDir, 0755); err != nil {
		return err
	}
	path := filepath.Join(m.markersDir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if os.IsExist(err) {
			return nil // Already exists, consider it successful
		}
		return err
	}
	return f.Close()
}

func (m *Markers) HasMarker(name string) (bool, error) {
	path := filepath.Join(m.markersDir, name)
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (m *Markers) HasAnyMarker() (bool, error) {
	entries, err := os.ReadDir(m.markersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".marker") {
			return true, nil
		}
	}
	return false, nil
}

func (m *Markers) DeleteAllMarkers() error {
	// we don't create dir for nonexist
	entries, err := os.ReadDir(m.markersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".marker") {
			err = os.Remove(filepath.Join(m.markersDir, entry.Name()))
			if err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}
