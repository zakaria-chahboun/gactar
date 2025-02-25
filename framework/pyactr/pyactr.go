// Package pyactr provides functions to output the internal actr data structures in Python suitable
// for running using the pyactr package, and to run those models using Python.
package pyactr

import (
	_ "embed"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/asmaloney/gactar/actr"
	"github.com/asmaloney/gactar/framework"

	"github.com/asmaloney/gactar/util/cli"
	"github.com/asmaloney/gactar/util/executil"
	"github.com/asmaloney/gactar/util/filesystem"
	"github.com/asmaloney/gactar/util/issues"
	"github.com/asmaloney/gactar/util/numbers"
)

//go:embed pyactr_print.py
var pyactrPrintPython string

var Info framework.Info = framework.Info{
	Name:           "pyactr",
	Language:       "python",
	FileExtension:  "py",
	ExecutableName: "python",

	PythonRequiredPackages: []string{"pyactr"},
}

type PyACTR struct {
	framework.Framework
	framework.WriterHelper

	tmpPath string

	model     *actr.Model
	className string
}

// New simply creates a new PyACTR instance and sets the tmp path from the context.
func New(settings *cli.Settings) (p *PyACTR, err error) {
	p = &PyACTR{tmpPath: settings.TempPath}

	err = framework.Setup(&Info)
	if err != nil {
		p = nil
		return
	}

	return
}

func (PyACTR) Info() *framework.Info {
	return &Info
}

func (PyACTR) ValidateModel(model *actr.Model) (log *issues.Log) {
	log = issues.New()

	if model.Memory.FinstTime != nil {
		log.Warning(nil, "pyactr does not support memory module's finst_time")
	}

	for _, production := range model.Productions {
		numPrintStatements := 0
		if production.DoStatements != nil {
			for _, statement := range production.DoStatements {
				if statement.Print == nil {
					continue
				}

				numPrintStatements++
				if numPrintStatements > 1 {
					location := issues.Location{
						Line:        production.AMODLineNumber,
						ColumnStart: 0,
						ColumnEnd:   0,
					}
					log.Warning(&location, "pyactr currently only supports one print statement per production (in '%s')", production.Name)
				}
			}
		}
	}

	return
}

func (p *PyACTR) SetModel(model *actr.Model) (err error) {
	if model.Name == "" {
		err = framework.ErrModelMissingName
		return
	}

	p.model = model
	p.className = fmt.Sprintf("pyactr_%s", p.model.Name)

	return
}

func (p PyACTR) Model() (model *actr.Model) {
	return p.model
}

func (p *PyACTR) Run(initialBuffers framework.InitialBuffers) (result *framework.RunResult, err error) {
	runFile, err := p.WriteModel(p.tmpPath, initialBuffers)
	if err != nil {
		return
	}

	result = &framework.RunResult{
		FileName:      runFile,
		GeneratedCode: p.GetContents(),
	}

	// run it!
	output, err := executil.ExecCommand(Info.ExecutableName, runFile)
	output = removeWarning(output)
	if err != nil {
		err = &executil.ErrExecuteCommand{Output: output}
		return
	}

	result.Output = []byte(output)

	return
}

// WriteModel converts the internal actr.Model to Python and writes it to a file.
func (p *PyACTR) WriteModel(path string, initialBuffers framework.InitialBuffers) (outputFileName string, err error) {
	// If our model has a print statement, then write out our support file
	if p.model.HasPrintStatement() {
		err = writePrintSupportFile(path, "pyactr_print.py")
		if err != nil {
			return
		}
	}

	outputFileName = fmt.Sprintf("%s.py", p.className)
	if path != "" {
		outputFileName = fmt.Sprintf("%s/%s", path, outputFileName)
	}

	err = filesystem.RemoveFile(outputFileName)
	if err != nil {
		return "", err
	}

	_, err = p.GenerateCode(initialBuffers)
	if err != nil {
		return
	}

	err = p.WriteFile(outputFileName)
	if err != nil {
		return
	}

	return
}

// GenerateCode converts the internal actr.Model to Python code.
func (p *PyACTR) GenerateCode(initialBuffers framework.InitialBuffers) (code []byte, err error) {
	patterns, err := framework.ParseInitialBuffers(p.model, initialBuffers)
	if err != nil {
		return
	}

	goal := patterns["goal"]

	err = p.InitWriterHelper()
	if err != nil {
		return
	}

	p.writeHeader()

	p.writeImports()

	p.Writeln("")

	// random
	if p.model.RandomSeed != nil {
		p.Writeln("numpy.random.seed(%d)\n", *p.model.RandomSeed)
	}

	memory := p.model.Memory
	p.Writeln("%s = actr.ACTRModel(", p.className)

	// enable subsymbolic computations
	p.Writeln("    subsymbolic=True,")

	if memory.LatencyFactor != nil {
		p.Writeln("    latency_factor=%s,", numbers.Float64Str(*memory.LatencyFactor))
	}

	if memory.LatencyExponent != nil {
		p.Writeln("    latency_exponent=%s,", numbers.Float64Str(*memory.LatencyExponent))
	}

	if memory.RetrievalThreshold != nil {
		p.Writeln("    retrieval_threshold=%s,", numbers.Float64Str(*memory.RetrievalThreshold))
	}

	if memory.Decay != nil {
		p.Writeln("    decay=%s,", numbers.Float64Str(*memory.Decay))
	}

	if memory.MaxSpreadStrength != nil {
		p.Writeln("    strength_of_association=%s,", numbers.Float64Str(*memory.MaxSpreadStrength))

		goalActivation := p.model.Goal.SpreadingActivation
		if goalActivation != nil {
			p.Writeln("    buffer_spreading_activation={'g': %s},", numbers.Float64Str(*goalActivation))
		}
	}

	if memory.InstantaneousNoise != nil {
		p.Writeln("    instantaneous_noise=%s,", numbers.Float64Str(*memory.InstantaneousNoise))
	}

	if memory.MismatchPenalty != nil {
		p.Writeln("    partial_matching=True, mismatch_penalty=%s,", numbers.Float64Str(*memory.MismatchPenalty))
	}

	procedural := p.model.Procedural
	if procedural.DefaultActionTime != nil {
		p.Writeln("    rule_firing=%s,", numbers.Float64Str(*procedural.DefaultActionTime))
	}

	if p.model.TraceActivations {
		p.Writeln("    activation_trace=True,")
	}

	p.Writeln(")")

	if p.model.HasPrintStatement() {
		p.Writeln("")
		p.Writeln("# pyactr doesn't handle general printing, so use gactar to add this capability")
		p.Writeln("pyactr_print.set_model(%s)", p.className)
	}

	p.Write("\n")

	// chunks
	for _, chunk := range p.model.Chunks {
		if chunk.IsInternal() {
			continue
		}

		p.Writeln("# amod line %d", chunk.AMODLineNumber)
		p.Writeln("actr.chunktype('%s', '%s')", chunk.TypeName, strings.Join(chunk.SlotNames, ", "))
	}
	p.Writeln("")

	// modules
	p.Writeln("%s = %s.decmem", memory.ModuleName(), p.className)

	if memory.FinstSize != nil {
		p.Writeln("%s.finst = %d", memory.ModuleName(), *memory.FinstSize)
	}

	p.Writeln("goal = %s.set_goal('goal')", p.className)

	imaginal := p.model.ImaginalModule()
	if imaginal != nil {
		p.Write(`imaginal = %s.set_goal(name="imaginal"`, p.className)
		if imaginal.Delay != nil {
			p.Write(", delay=%s", numbers.Float64Str(*imaginal.Delay))
		}
		p.Writeln(")")
	}

	// add any extra buffers
	extraBuffers := p.model.LookupModule("extra_buffers")
	if extraBuffers != nil {
		p.Writeln("")
		p.Writeln("# define a goal-style buffer for each extra buffer")
		for _, buff := range extraBuffers.BufferNames() {
			p.Writeln("%[1]s = %[2]s.set_goal('%[1]s')", buff, p.className)
		}
	}

	p.Writeln("")

	p.writeInitializers(goal)

	p.writeSimilarities()

	// Add user-set goal if any
	if goal != nil {
		p.Writeln("goal.add(actr.chunkstring(string='''")
		p.outputPattern(goal, 1)
		p.Writeln("'''))")
		p.Writeln("")
	}

	p.writeProductions()

	p.Writeln("")

	// ...add our code to run
	p.writeMain()

	code = p.GetContents()
	return
}

// writePrintSupportFile will write out a Python file to add extra print support to pyactr.
func writePrintSupportFile(path, supportFileName string) (err error) {
	if path != "" {
		supportFileName = fmt.Sprintf("%s/%s", path, supportFileName)
	}

	file, err := os.OpenFile(supportFileName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0660)
	if err != nil {
		return
	}
	defer file.Close()

	_, err = file.WriteString(pyactrPrintPython)
	if err != nil {
		return
	}

	return
}

func (p PyACTR) writeHeader() {
	p.Writeln("# Generated by gactar %s", framework.GactarVersion)
	p.Writeln("#           on %s", framework.TimeNow().Format("2006-01-02 @ 15:04:05"))
	p.Writeln("#   https://github.com/asmaloney/gactar")
	p.Writeln("")
	p.Writeln("# *** NOTE: This is a generated file. Any changes may be overwritten.")
	p.Writeln("")

	if p.model.Description != "" {
		p.Write("# %s\n\n", p.model.Description)
	}

	p.writeAuthors()
}

func (p PyACTR) writeAuthors() {
	if len(p.model.Authors) == 0 {
		return
	}

	p.Writeln("# Authors:")

	for _, author := range p.model.Authors {
		p.Write("#     %s\n", author)
	}

	p.Writeln("")
}

func (p PyACTR) writeImports() {
	if p.model.RandomSeed != nil {
		p.Writeln("import numpy")
	}

	p.Writeln("import pyactr as actr")

	if p.model.HasPrintStatement() {
		// Import gactar's print handling
		p.Writeln("import pyactr_print")
	}
}

func (p PyACTR) writeImplicitChunks() {
	if !p.model.HasImplicitChunks() {
		return
	}

	p.Writeln("# declare implicit chunks to avoid warnings")
	p.Writeln("actr.chunktype('chunk', 'value')")
	for _, chunkName := range p.model.ImplicitChunks {
		p.Writeln("actr.makechunk(nameofchunk='%[1]s', typename='chunk', value='%[1]s')", chunkName)
	}

	p.Writeln("")
}

func (p PyACTR) writeInitializers(goal *actr.Pattern) {
	p.writeImplicitChunks()

	for _, init := range p.model.Initializers {
		module := init.Module

		// allow the user-set goal to override the initializer
		if module.ModuleName() == "goal" && (goal != nil) {
			continue
		}

		p.Writeln("# amod line %d", init.AMODLineNumber)

		if module.ModuleName() == "extra_buffers" {
			p.Write("%s.add(actr.chunkstring(", init.Buffer.BufferName())
		} else {
			p.Write("%s.add(actr.chunkstring(", module.ModuleName())
		}
		if init.ChunkName != nil {
			p.Write("name='%s', ", *init.ChunkName)
		}
		p.Writeln("string='''")
		p.outputPattern(init.Pattern, 1)
		p.Writeln("'''))")
	}

	p.Writeln("")
}

func (p PyACTR) writeSimilarities() {
	if len(p.model.Similarities) == 0 {
		return
	}

	for _, similar := range p.model.Similarities {
		p.Writeln("# amod line %d", similar.AMODLineNumber)
		p.Writeln("%s.set_similarities('%s', '%s', %s)", p.className, similar.ChunkOne, similar.ChunkTwo, numbers.Float64Str(similar.Value))
	}

	p.Writeln("")
}

func (p PyACTR) writeProductions() {
	for _, production := range p.model.Productions {
		if production.Description != nil {
			p.Writeln("# %s", *production.Description)
		}

		p.Writeln("# amod line %d", production.AMODLineNumber)

		p.Writeln("%s.productionstring(name='%s', string='''", p.className, production.Name)
		for _, match := range production.Matches {
			p.outputMatch(match)
		}

		p.Writeln("     ==>")

		if production.DoStatements != nil {
			for _, statement := range production.DoStatements {
				p.outputStatement(production, statement)
			}
		}

		p.Write("''')\n\n")
	}
}

func (p PyACTR) writeMain() {
	p.Writeln("# Main")
	p.Writeln("if __name__ == '__main__':")
	p.Writeln("    sim = %s.simulation()", p.className)
	p.Writeln("    sim.run()")
	// TODO: Add some intelligent output when logging level is info or detail
	p.Writeln("    if goal.test_buffer('full') is True:")
	p.Writeln("        print('final goal: ' + str(goal.pop()))")
}

func (p PyACTR) outputPattern(pattern *actr.Pattern, tabs int) {
	tabbedItems := framework.KeyValueList{}
	tabbedItems.Add("isa", pattern.Chunk.TypeName)

	for i, slot := range pattern.Slots {
		slotName := pattern.Chunk.SlotNames[i]
		addPatternSlot(&tabbedItems, slotName, slot)
	}

	p.TabWrite(tabs, tabbedItems)
}

func (p PyACTR) outputMatch(match *actr.Match) {
	bufferName := match.Buffer.BufferName()
	chunkName := match.Pattern.Chunk.TypeName

	if actr.IsInternalChunkType(chunkName) {
		if chunkName == "_status" {
			status := match.Pattern.Slots[0]
			p.Writeln("     ?%s>", bufferName)

			// Table 2.1 page 24 of pyactr book
			if status.String() == "full" || status.String() == "empty" {
				p.Writeln("          buffer %s", status)
			} else {
				p.Writeln("          state %s", status)
			}
		}
	} else {
		p.Writeln("     =%s>", bufferName)
		p.outputPattern(match.Pattern, 2)
	}
}

func addPatternSlot(tabbedItems *framework.KeyValueList, slotName string, slot *actr.PatternSlot) {
	if slot.Wildcard {
		return
	}

	var value string
	if slot.Negated {
		value = "~"
	}

	switch {
	case slot.Nil:
		value += "None"

	case slot.ID != nil:
		value += *slot.ID

	case slot.Str != nil:
		value += fmt.Sprintf("%q", *slot.Str)

	case slot.Num != nil:
		value += *slot.Num

	case slot.Var != nil:
		value += "="
		value += strings.TrimPrefix(*slot.Var.Name, "?")
	}

	tabbedItems.Add(slotName, value)

	// Check for constraints on a var and output them
	if slot.Var != nil {
		if len(slot.Var.Constraints) > 0 {
			for _, constraint := range slot.Var.Constraints {
				// default to equality
				value := ""

				if constraint.Comparison == actr.NotEqual {
					value = "~"
				}

				if constraint.RHS.Var != nil {
					value += "="
					value += strings.TrimPrefix(*constraint.RHS.Var, "?")

				} else {
					value += constraint.RHS.String()
				}

				tabbedItems.Add(slotName, value)
			}
		}
	}
}

func (p PyACTR) outputStatement(production *actr.Production, s *actr.Statement) {
	switch {
	case s.Set != nil:
		buffer := s.Set.Buffer
		bufferName := buffer.BufferName()

		p.Write("     =%s>\n", bufferName)

		if s.Set.Slots != nil {
			tabbedItems := framework.KeyValueList{}
			tabbedItems.Add("isa", s.Set.Chunk.TypeName)

			for _, slot := range *s.Set.Slots {
				slotName := slot.Name

				switch {
				case slot.Value.Nil != nil:
					tabbedItems.Add(slotName, "None")

				case slot.Value.Var != nil:
					tabbedItems.Add(slotName, fmt.Sprintf("=%s", *slot.Value.Var))

				case slot.Value.ID != nil:
					tabbedItems.Add(slotName, *slot.Value.ID)

				case slot.Value.Number != nil:
					tabbedItems.Add(slotName, *slot.Value.Number)

				case slot.Value.Str != nil:
					tabbedItems.Add(slotName, fmt.Sprintf(`%q`, *slot.Value.Str))
				}
			}
			p.TabWrite(2, tabbedItems)
		} else if s.Set.Pattern != nil {
			p.outputPattern(s.Set.Pattern, 2)
		}

	case s.Recall != nil:
		// Clear the buffer before we set it
		// See: https://github.com/jakdot/pyactr/issues/9#issuecomment-940442787
		p.Writeln("     ~retrieval>")
		p.Writeln("     +retrieval>")
		p.outputPattern(s.Recall.Pattern, 2)

	case s.Print != nil:
		// Using "goal" here is arbitrary because of the way we monkey patch the python code.
		// Our "print_text" statement handles its own formatting and lookup.
		p.Writeln("     !goal>")

		str := make([]string, len(*s.Print.Values))

		for index, val := range *s.Print.Values {
			switch {
			case val.Var != nil:
				varIndex := production.VarIndexMap[*val.Var]
				str[index] = fmt.Sprintf("%s.%s", varIndex.Buffer.BufferName(), varIndex.SlotName)

			case val.Str != nil:
				str[index] = fmt.Sprintf("'%s'", *val.Str)

			case val.Number != nil:
				str[index] = *val.Number
			}
		}

		p.Writeln("          print_text \"%s\"", strings.Join(str, ", "))

	case s.Clear != nil:
		for _, name := range s.Clear.BufferNames {
			p.Writeln("     ~%s>", name)
		}

	case s.Stop != nil:
		// to stop in pyactr, clear the goal buffer
		p.Writeln("     ~goal>")
	}
}

// removeWarning will remove the long warning whenever pyactr is run without tkinter.
func removeWarning(text string) string {
	r := regexp.MustCompile(`(?s).+warnings.warn\("Simulation GUI is set to False."\)(.+)`)
	matches := r.FindAllStringSubmatch(text, -1)
	if len(matches) == 1 {
		text = strings.TrimSpace(matches[0][1])
	}

	return text
}
