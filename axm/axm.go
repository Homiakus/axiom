// Package axm implements the optional AXM frontend.
package axm

import (
	"fmt"
	"os"

	"github.com/Homiakus/axiom"
)

type Source struct {
	Data []byte
	Name string
}

func Bytes(data []byte) Source {
	return Source{Data: append([]byte(nil), data...), Name: "inline.axm"}
}

func Named(name string, data []byte) Source {
	return Source{Data: append([]byte(nil), data...), Name: name}
}

func Load(path string) (*axiom.Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Named(path, data).CompilePlan()
}

func Parse(data []byte) (*axiom.Plan, error) {
	return Bytes(data).CompilePlan()
}

func (s Source) CompilePlan() (*axiom.Plan, error) {
	if len(s.Data) == 0 {
		return nil, fmt.Errorf("axm: source is empty")
	}
	name := s.Name
	if name == "" {
		name = "inline.axm"
	}
	plan, err := axiom.CompilePlan(s.Data, axiom.WithSourceName(name))
	if err != nil {
		return nil, err
	}
	plan.Format = "axm"
	return plan, nil
}
