package modules

import "github.com/asmaloney/gactar/actr/buffer"

type Procedural struct {
	buffer.BufferInterface // unused

	// "default_action_time": time that it takes to fire a production (seconds)
	// ccm: 0.05
	// pyactr: 0.05
	// vanilla: 0.05
	DefaultActionTime *float64
}

func NewProcedural() *Procedural {
	return &Procedural{BufferInterface: buffer.Buffer{}}
}

func (Procedural) ModuleName() string {
	return "procedural"
}
