package manifest

import (
	"errors"
	"github.com/onepanelio/cli/util"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type Manifest struct {
	path       string // where the manifest directory is located
	components map[string]*Component
	overlays   map[string]*Overlay
}

func LoadManifest(manifestRoot string) (*Manifest, error) {
	m := &Manifest{
		path:       manifestRoot,
		components: make(map[string]*Component),
		overlays:   make(map[string]*Overlay),
	}

	err := filepath.Walk(manifestRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Don't consider individual files
		if !info.IsDir() {
			return nil
		}

		relativePath, relErr := filepath.Rel(manifestRoot, path)
		if relErr != nil {
			log.Printf("Relative Path: err %v", relErr)
			return relErr
		}

		parts := strings.Split(relativePath, string(os.PathSeparator))

		for i, part := range parts {
			if part == "base" {
				pathUpToBase := strings.Join(parts[:i], string(os.PathSeparator))
				m.addComponent(pathUpToBase)
			}

			if i > 0 && parts[i-1] == "overlays" {
				pathUpToOverlay := strings.Join(parts[:i+1], string(os.PathSeparator))
				m.addOverlay(pathUpToOverlay)
			}
		}

		return nil
	})

	return m, err
}

// relative path: something (part of something/base)
func (m *Manifest) addComponent(relativePath string) *Component {
	component, ok := m.components[relativePath]
	if ok {
		return component
	}

	newComponent := CreateComponent(relativePath)
	m.components[relativePath] = newComponent

	return newComponent
}

// relative path: something/overlays/something2
func (m *Manifest) addOverlay(relativePath string) *Overlay {
	overlay, ok := m.overlays[relativePath]
	if ok {
		return overlay
	}

	startOfOverlaysIndex := strings.LastIndex(relativePath, "overlays")
	componentPath := relativePath[:startOfOverlaysIndex-1]

	component := m.addComponent(componentPath)

	overlay = CreateOverlay(relativePath, component)
	component.AddOverlay(overlay)

	m.overlays[relativePath] = overlay

	return overlay
}

func (m *Manifest) GetComponent(path string) *Component {
	return m.components[path]
}

func (m *Manifest) GetOverlay(path string) *Overlay {
	return m.overlays[path]
}

// Validate checks if the manifest is valid. If it is, nil is returned. Otherwise an error is returned.
func Validate(manifest *util.DynamicYaml) error {
	defaultNamespace := manifest.GetValue("application.defaultNamespace")
	if defaultNamespace == nil {
		return errors.New("application.defaultNamespace.missing")
	}

	if defaultNamespace.Value == "" {
		return errors.New("application.defaultNamespace.blank")
	}
	if defaultNamespace.Value == "onepanel" {
		return errors.New("application.defaultNamespace.reserved")
	}

	return nil
}
