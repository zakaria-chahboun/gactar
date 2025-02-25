// Package ccm_pyactr provides functions to output the internal actr data structures in Python suitable
// for running using CCM's python_actr package, and to run those models using Python.
package ccm_pyactr

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"github.com/asmaloney/gactar/actr"
	"github.com/asmaloney/gactar/framework"

	"github.com/asmaloney/gactar/util/cli"
	"github.com/asmaloney/gactar/util/executil"
	"github.com/asmaloney/gactar/util/filesystem"
	"github.com/asmaloney/gactar/util/issues"
	"github.com/asmaloney/gactar/util/numbers"
)

//go:embed gactar_ccm_activate_trace.py
var gactarActivateTraceFile string

const gactarActivateTraceFileName = "gactar_ccm_activate_trace"

var Info framework.Info = framework.Info{
	Name:           "ccm",
	Language:       "python",
	FileExtension:  "py",
	ExecutableName: "python",

	PythonRequiredPackages: []string{"python_actr"},
}

type CCMPyACTR struct {
	framework.Framework
	framework.WriterHelper

	tmpPath string

	model     *actr.Model
	className string
}

// New simply creates a new CCMPyACTR instance and sets the tmp path.
func New(settings *cli.Settings) (c *CCMPyACTR, err error) {
	c = &CCMPyACTR{tmpPath: settings.TempPath}

	err = framework.Setup(&Info)
	if err != nil {
		c = nil
		return
	}

	return
}

func (CCMPyACTR) Info() *framework.Info {
	return &Info
}

func (CCMPyACTR) ValidateModel(model *actr.Model) (log *issues.Log) {
	log = issues.New()

	if model.Memory.LatencyExponent != nil {
		log.Warning(nil, "ccm does not support memory module's latency_exponent")
	}

	return
}

// SetModel sets our model and saves the python class name we are going to use.
func (c *CCMPyACTR) SetModel(model *actr.Model) (err error) {
	if model.Name == "" {
		err = framework.ErrModelMissingName
		return
	}

	c.model = model
	c.className = fmt.Sprintf("ccm_%s", c.model.Name)

	return
}

func (c CCMPyACTR) Model() (model *actr.Model) {
	return c.model
}

// Run generates the python code from the amod file, writes it to disk, creates a "run" file
// to actually run the model, and returns the output (stdout and stderr combined).
func (c *CCMPyACTR) Run(initialBuffers framework.InitialBuffers) (result *framework.RunResult, err error) {
	runFile, err := c.WriteModel(c.tmpPath, initialBuffers)
	if err != nil {
		return
	}

	result = &framework.RunResult{
		FileName:      runFile,
		GeneratedCode: c.GetContents(),
	}

	output, err := executil.ExecCommand(Info.ExecutableName, runFile)
	if err != nil {
		return
	}

	result.Output = []byte(output)

	return
}

// WriteModel converts the internal actr.Model to Python and writes it to a file.
func (c *CCMPyACTR) WriteModel(path string, initialBuffers framework.InitialBuffers) (outputFileName string, err error) {
	// If our model is tracing activations, then write out our support file
	if c.model.TraceActivations {
		err = writeTraceSupportFile(path)
		if err != nil {
			return
		}
	}

	outputFileName = fmt.Sprintf("%s.py", c.className)
	if path != "" {
		outputFileName = fmt.Sprintf("%s/%s", path, outputFileName)
	}

	err = filesystem.RemoveFile(outputFileName)
	if err != nil {
		return "", err
	}

	_, err = c.GenerateCode(initialBuffers)
	if err != nil {
		return
	}

	err = c.WriteFile(outputFileName)
	if err != nil {
		return
	}

	return
}

// GenerateCode converts the internal actr.Model to Python code.
func (c *CCMPyACTR) GenerateCode(initialBuffers framework.InitialBuffers) (code []byte, err error) {
	patterns, err := framework.ParseInitialBuffers(c.model, initialBuffers)
	if err != nil {
		return
	}

	goal := patterns["goal"]

	err = c.InitWriterHelper()
	if err != nil {
		return
	}

	c.writeHeader()

	memory := c.model.Memory

	c.writeImports()

	c.Write("\n\n")

	// random
	if c.model.RandomSeed != nil {
		c.Writeln("random.seed(%d)", *c.model.RandomSeed)
		c.Write("\n\n")
	}

	c.Writeln("class %s(ACTR):", c.className)

	for _, buffer := range c.model.BufferNames() {
		c.Writeln("    %s = Buffer()", buffer)
	}

	c.Writeln("")

	additionalInit := []string{}

	if memory.LatencyFactor != nil {
		additionalInit = append(additionalInit, fmt.Sprintf("latency=%s", numbers.Float64Str(*memory.LatencyFactor)))
	}

	if memory.RetrievalThreshold != nil {
		additionalInit = append(additionalInit, fmt.Sprintf("threshold=%s", numbers.Float64Str(*memory.RetrievalThreshold)))
	}

	if memory.FinstSize != nil {
		additionalInit = append(additionalInit, fmt.Sprintf("finst_size=%d", *memory.FinstSize))
	}

	if memory.FinstTime != nil {
		additionalInit = append(additionalInit, fmt.Sprintf("finst_time=%s", numbers.Float64Str(*memory.FinstTime)))
	}

	if len(additionalInit) > 0 {
		c.Writeln("    %s = Memory(%s, %s)", memory.ModuleName(), memory.BufferName(), strings.Join(additionalInit, ", "))
	} else {
		c.Writeln("    %s = Memory(%s)", memory.ModuleName(), memory.BufferName())
	}

	if c.model.TraceActivations {
		c.Writeln("    trace = ActivateTrace(%s)", memory.ModuleName())
	}

	c.Writeln("")

	// Turn on DMBaseLevel if we have set "decay"
	if memory.Decay != nil {
		c.Writeln("    DMBaseLevel(%s, decay=%s)", memory.ModuleName(), numbers.Float64Str(*memory.Decay))
		c.Writeln("")
	}

	// Turn on DMSpreading if we have set "max_spread_strength"
	if memory.MaxSpreadStrength != nil {
		c.Writeln("    spread = DMSpreading(%s, goal)", memory.ModuleName())
		c.Writeln("    spread.strength = %s", numbers.Float64Str(*memory.MaxSpreadStrength))

		goalActivation := c.model.Goal.SpreadingActivation
		if goalActivation != nil {
			c.Writeln("    spread.weight[%s] = %s", "goal", numbers.Float64Str(*goalActivation))
		}

		c.Writeln("")
	}

	// Turn on DMNoise if we have set "instantaneous_noise"
	if memory.InstantaneousNoise != nil {
		c.Writeln("    DMNoise(%s, noise=%s)", memory.ModuleName(), numbers.Float64Str(*memory.InstantaneousNoise))
		c.Writeln("")
	}
	// Turn on Partial if we have set "mismatch_penalty"
	if memory.MismatchPenalty != nil {
		c.Writeln("    partial = Partial(%[1]s, limit=%s)", memory.ModuleName(), numbers.Float64Str(*memory.MismatchPenalty))
		c.writeSimilarities("partial")
		c.Writeln("")
	}

	procedural := c.model.Procedural

	if procedural.DefaultActionTime != nil {
		c.Writeln("    production_time = %s", numbers.Float64Str(*procedural.DefaultActionTime))
		c.Writeln("")
	}

	if c.model.LogLevel == "info" {
		// this turns on some logging at the high level
		c.Writeln("    def __init__(self):")
		c.Writeln("        super().__init__(log=True)")
		c.Writeln("")
	}

	c.writeInitializers(goal)

	c.Writeln("")

	// Add user-set goal if any
	if goal != nil {
		c.Write("        goal.set(")
		c.outputPattern(goal)
		c.Write(")\n\n")
	}

	c.writeProductions()

	c.Writeln("")

	c.writeMain()

	code = c.GetContents()
	return
}

// writeTraceSupportFile will write out a Python file to add minimal activation trace support.
func writeTraceSupportFile(path string) (err error) {
	supportFileName := fmt.Sprintf("%s.py", gactarActivateTraceFileName)
	if path != "" {
		supportFileName = fmt.Sprintf("%s/%s", path, supportFileName)
	}

	file, err := os.OpenFile(supportFileName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0660)
	if err != nil {
		return
	}
	defer file.Close()

	_, err = file.WriteString(gactarActivateTraceFile)
	if err != nil {
		return
	}

	return
}

func (c CCMPyACTR) writeHeader() {
	c.Writeln("# Generated by gactar %s", framework.GactarVersion)
	c.Writeln("#           on %s", framework.TimeNow().Format("2006-01-02 @ 15:04:05"))
	c.Writeln("#   https://github.com/asmaloney/gactar")
	c.Writeln("")
	c.Writeln("# *** NOTE: This is a generated file. Any changes may be overwritten.")
	c.Writeln("")

	if c.model.Description != "" {
		c.Write("# %s\n\n", c.model.Description)
	}

	c.writeAuthors()
}

func (c CCMPyACTR) writeAuthors() {
	if len(c.model.Authors) == 0 {
		return
	}

	c.Writeln("# Authors:")

	for _, author := range c.model.Authors {
		c.Write("#    %s\n", author)
	}

	c.Writeln("")
}

func (c CCMPyACTR) writeImports() {
	if c.model.RandomSeed != nil {
		c.Writeln("import random")
	}

	memory := c.model.Memory

	imports := []string{"ACTR", "Buffer", "Memory"}

	c.Write("from python_actr import %s\n", strings.Join(imports, ", "))

	additionalImports := []string{}

	if memory.Decay != nil {
		additionalImports = append(additionalImports, "DMBaseLevel")
	}

	if memory.MaxSpreadStrength != nil {
		additionalImports = append(additionalImports, "DMSpreading")
	}

	if memory.InstantaneousNoise != nil {
		additionalImports = append(additionalImports, "DMNoise")
	}

	if memory.MismatchPenalty != nil {
		additionalImports = append(additionalImports, "Partial")
	}

	if len(additionalImports) > 0 {
		c.Write("from python_actr import %s\n", strings.Join(additionalImports, ", "))
	}

	if c.model.LogLevel == "detail" {
		c.Writeln("from python_actr import log, log_everything")
	}

	if c.model.TraceActivations {
		c.Writeln("")
		c.Writeln(fmt.Sprintf("from %s import ActivateTrace", gactarActivateTraceFileName))
	}
}

func (c CCMPyACTR) writeInitializers(goal *actr.Pattern) {
	if len(c.model.Initializers) == 0 {
		return
	}

	c.Writeln("    def init():")

	for _, init := range c.model.Initializers {
		module := init.Module

		// allow the user-set goal to override the initializer
		if module.ModuleName() == "goal" && (goal != nil) {
			continue
		}

		c.Write("        # amod line %d", init.AMODLineNumber)
		if init.ChunkName != nil {
			c.Write(" '%s'", *init.ChunkName)
		}
		c.Writeln("")

		buffer := init.Buffer
		if buffer.AllowsMultipleInit() {
			c.Write("        %s.add(", module.ModuleName())
		} else {
			c.Write("        %s.set(", buffer.BufferName())
		}

		c.outputPattern(init.Pattern)
		c.Writeln(")")
	}
}

func (c CCMPyACTR) writeSimilarities(partialName string) {
	if len(c.model.Similarities) == 0 {
		return
	}

	for _, similar := range c.model.Similarities {
		c.Writeln("    # amod line %d", similar.AMODLineNumber)
		c.Writeln("    %s.similarity('%s', '%s', %s)", partialName, similar.ChunkOne, similar.ChunkTwo, numbers.Float64Str(similar.Value))
	}
}

func (c CCMPyACTR) writeProductions() {
	for _, production := range c.model.Productions {
		if production.Description != nil {
			c.Writeln("    # %s", *production.Description)
		}

		c.Writeln("    # amod line %d", production.AMODLineNumber)

		c.Write("    def %s(", production.Name)

		numMatches := len(production.Matches)
		for i, match := range production.Matches {
			c.outputMatch(match)

			if i != numMatches-1 {
				c.Write(", ")
			}
		}

		c.Writeln("):")

		if production.DoStatements != nil {
			for _, statement := range production.DoStatements {
				c.outputStatement(statement)
			}
		}

		c.Write("\n")
	}
}

func (c CCMPyACTR) writeMain() {
	c.Writeln("if __name__ == \"__main__\":")
	c.Writeln(fmt.Sprintf("    model = %s()", c.className))

	if c.model.LogLevel == "detail" {
		c.Writeln("    log(summary=1)")
		c.Writeln("    log_everything(model)")
	}

	c.Writeln("    model.run()")
}

func (c CCMPyACTR) outputPattern(pattern *actr.Pattern) {
	str := fmt.Sprintf("'%s ", pattern.Chunk.TypeName)

	for i, slot := range pattern.Slots {
		str += patternSlotString(slot)

		if i != len(pattern.Slots)-1 {
			str += " "
		}
	}

	str += "'"

	c.Write(str)
}

func (c CCMPyACTR) outputMatch(match *actr.Match) {
	var name string
	if match.Buffer != nil {
		name = match.Buffer.BufferName()
	}

	chunkTypeName := match.Pattern.Chunk.TypeName
	if actr.IsInternalChunkType(chunkTypeName) {
		if chunkTypeName == "_status" {
			status := match.Pattern.Slots[0]
			if name == "retrieval" {
				name = "memory"
			}
			c.Write("%s='%s:True'", name, status)
		}
	} else {
		c.Write("%s=", name)
		c.outputPattern(match.Pattern)
	}
}

func patternSlotString(slot *actr.PatternSlot) string {
	var str string

	if slot.Negated {
		str += "!"
	}

	switch {
	case slot.Wildcard:
		str += "?"

	case slot.Nil:
		str += "None"

	case slot.ID != nil:
		str += *slot.ID

	case slot.Str != nil:
		str += strings.ReplaceAll(*slot.Str, " ", "_")

	case slot.Var != nil:
		str += *slot.Var.Name

	case slot.Num != nil:
		str += *slot.Num
	}

	// Check for constraints on a var and output them
	if slot.Var != nil {
		if len(slot.Var.Constraints) > 0 {
			for _, constraint := range slot.Var.Constraints {
				if constraint.Comparison == actr.NotEqual {
					str += "!"
				}

				str += convertValue(constraint.RHS)
			}
		}
	}

	return str
}

func (c CCMPyACTR) outputStatement(s *actr.Statement) {
	switch {
	case s.Set != nil:
		if s.Set.Slots != nil {
			slotAssignments := []string{}
			for _, slot := range *s.Set.Slots {
				value := convertValue(slot.Value)
				slotAssignments = append(slotAssignments, fmt.Sprintf("_%d=%s", slot.SlotIndex, value))
			}
			c.Writeln("        %s.modify(%s)", s.Set.Buffer.BufferName(), strings.Join(slotAssignments, ", "))
		} else {
			c.Write("        %s.set(", s.Set.Buffer.BufferName())
			c.outputPattern(s.Set.Pattern)
			c.Writeln(")")
		}

	case s.Recall != nil:
		c.Write("        %s.request(", s.Recall.MemoryName)
		c.outputPattern(s.Recall.Pattern)
		c.Writeln(")")

	case s.Clear != nil:
		for _, name := range s.Clear.BufferNames {
			c.Writeln("        %s.clear()", name)
		}

	case s.Print != nil:
		values := pythonValuesToStrings(s.Print.Values, true)
		c.Writeln("        print(%s, sep='')", strings.Join(values, ", "))

	case s.Stop != nil:
		c.Writeln("        self.stop()")
	}
}

func convertValue(s *actr.Value) string {
	switch {
	case s.Nil != nil:
		return "None"

	case s.Var != nil:
		return *s.Var

	case s.ID != nil:
		return "'" + *s.ID + "'"

	case s.Number != nil:
		return *s.Number

	case s.Str != nil:
		return "'" + strings.ReplaceAll(*s.Str, " ", "_") + "'"
	}

	return ""
}

func pythonValuesToStrings(values *[]*actr.Value, quoteStrings bool) []string {
	str := make([]string, len(*values))
	for i, v := range *values {
		switch {
		case v.Var != nil:
			str[i] = strings.TrimPrefix(*v.Var, "?")

		case v.Str != nil:
			if quoteStrings {
				str[i] = fmt.Sprintf("'%s'", *v.Str)
			} else {
				str[i] = *v.Str
			}

		case v.Number != nil:
			str[i] = *v.Number
		}
		// v.ID && v.Nil should not be possible because of validation
	}

	return str
}
