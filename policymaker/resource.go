package policymaker

import "fmt"

// Resource is a helper type
type Resource struct {
	Type string
	Mode Mode
}

// Mode represents a specific resource meta type
type Mode string

// List of available qualifiers.
const (
	ModeManaged Mode = "resource"
	ModeData    Mode = "data"
)

// NewResource is a Constructor for Resource
func NewResource(t string, m string) *Resource {
	mode := ModeManaged
	if m == "data" {
		mode = ModeData
	}
	return &Resource{Type: t, Mode: mode}
}

//ToString is a helper function
func (r *Resource) ToString() string {
	return fmt.Sprintf(`%s_%s`, r.Mode, r.Type)
}
