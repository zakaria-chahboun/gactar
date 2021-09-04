package ccm_pyactr

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/urfave/cli/v2"

	"gitlab.com/asmaloney/gactar/actr"
	"gitlab.com/asmaloney/gactar/amod"
	"gitlab.com/asmaloney/gactar/framework"
)

type CCMPyACTR struct {
	framework.WriterHelper
	model     *actr.Model
	className string
	tmpPath   string
}

// New simply creates a new CCMPyACTR instance and sets the tmp path.
func New(cli *cli.Context) (c *CCMPyACTR, err error) {

	c = &CCMPyACTR{tmpPath: "tmp"}

	return
}

// Initialize will check for python3 and the ccm package, and create a tmp dir to save files for running.
// Note that this directory is not currently created in the proper place - it should end up in the OS's
// tmp directory. It is created locally so we can look at and debug the generated python files.
func (c *CCMPyACTR) Initialize() (err error) {
	_, err = framework.CheckForExecutable("python3")
	if err != nil {
		return
	}

	framework.IdentifyYourself("ccm", "python3")

	err = framework.PythonCheckForPackage("ccm")
	if err != nil {
		return
	}

	err = os.MkdirAll(c.tmpPath, os.ModePerm)
	if err != nil {
		return
	}

	return
}

// SetModel sets our model and saves the python class name we are going to use.
func (c *CCMPyACTR) SetModel(model *actr.Model) (err error) {
	if model.Name == "" {
		err = fmt.Errorf("model is missing name")
		return
	}

	c.model = model
	c.className = fmt.Sprintf("ccm_%s", c.model.Name)

	return
}

// Run generates the python code from the amod file, writes it to disk, creates a "run" file
// to actually run the model, and returns the output (stdout and stderr combined).
func (c *CCMPyACTR) Run(initialGoal string) (generatedCode, output []byte, err error) {
	runFile, err := c.WriteModel(c.tmpPath, initialGoal)
	if err != nil {
		return
	}

	cmd := exec.Command("python3", runFile)

	output, err = cmd.CombinedOutput()
	if err != nil {
		err = fmt.Errorf("%s", string(output))
		return
	}

	generatedCode = c.GetContents()

	return
}

// WriteModel converts the internal actr.Model to python and writes it to a file.
func (c *CCMPyACTR) WriteModel(path, initialGoal string) (outputFileName string, err error) {
	goal, err := amod.ParseChunk(c.model, initialGoal)
	if err != nil {
		err = fmt.Errorf("error in initial goal - %s", err)
		return
	}

	outputFileName = fmt.Sprintf("%s.py", c.className)
	if path != "" {
		outputFileName = fmt.Sprintf("%s/%s", path, outputFileName)
	}

	err = c.InitWriterHelper(outputFileName)
	if err != nil {
		return
	}
	defer c.CloseWriterHelper()

	c.Writeln("# This file is generated by gactar %s", time.Now().Format("2006-01-02 15:04:05"))
	c.Writeln("# https://github.com/asmaloney/gactar")
	c.Writeln("")
	c.Writeln("# *** This is a generated file. Any changes may be overwritten.")
	c.Writeln("")
	c.Write("# %s\n\n", c.model.Description)

	imports := []string{"ACTR", "Buffer", "Memory"}

	c.Write("from ccm.lib.actr import %s\n", strings.Join(imports, ", "))

	if c.model.LogLevel == "detail" {
		c.Writeln("from ccm import log, log_everything")
	}

	c.Write("\n\n")

	c.Writeln("class %s(ACTR):", c.className)

	for _, buf := range c.model.Buffers {
		c.Writeln("\t%s = Buffer()", buf.GetName())
	}

	memory := c.model.Memory
	additionalInit := []string{}

	if memory.Latency != nil {
		additionalInit = append(additionalInit, fmt.Sprintf("latency=%s", framework.Float64Str(*memory.Latency)))
	}

	if memory.Threshold != nil {
		additionalInit = append(additionalInit, fmt.Sprintf("threshold=%s", framework.Float64Str(*memory.Threshold)))
	}

	if memory.MaxTime != nil {
		additionalInit = append(additionalInit, fmt.Sprintf("maximum_time=%s", framework.Float64Str(*memory.MaxTime)))
	}

	if memory.FinstSize != nil {
		additionalInit = append(additionalInit, fmt.Sprintf("finst_size=%d", *memory.FinstSize))
	}

	if memory.FinstTime != nil {
		additionalInit = append(additionalInit, fmt.Sprintf("finst_time=%s", framework.Float64Str(*memory.FinstTime)))
	}

	if len(additionalInit) > 0 {
		c.Writeln("\t%s = Memory(%s, %s)", memory.Name, memory.Buffer.GetName(), strings.Join(additionalInit, ", "))
	} else {
		c.Writeln("\t%s = Memory(%s)", memory.Name, memory.Buffer.GetName())
	}

	c.Writeln("")

	if c.model.LogLevel == "info" {
		// this turns on some logging at the high level
		c.Writeln("\tdef __init__(self):")
		c.Writeln("\t\tsuper().__init__(log=True)")
		c.Writeln("")
	}

	if len(c.model.Initializers) > 0 {
		c.Writeln("\tdef init():")

		for _, init := range c.model.Initializers {
			if init.Buffer != nil {
				initializer := init.Buffer.GetName()

				// allow the user-set goal to override the initializer
				if initializer == "goal" && (goal != nil) {
					continue
				}
				c.Write("\t\t%s.set(", initializer)

			} else { // memory
				c.Write("\t\t%s.add(", init.Memory.Name)
			}

			c.outputPattern(init.Pattern)
			c.Writeln(")")
		}

		c.Writeln("")
	}

	for _, production := range c.model.Productions {
		if production.Description != nil {
			c.Writeln("\t# %s", *production.Description)
		}

		c.Writeln("\t# amod line %d", production.AMODLineNumber)

		c.Write("\tdef %s(", production.Name)

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

	c.Writeln("")
	c.Writeln("if __name__ == \"__main__\":")
	c.Writeln(fmt.Sprintf("\tmodel = %s()", c.className))
	if goal != nil {
		c.Writeln(fmt.Sprintf("\tmodel.goal.set('%s')", convertGoal(goal)))
	}

	if c.model.LogLevel == "detail" {
		c.Writeln("\tlog(summary=1)")
		c.Writeln("\tlog_everything(model)")
	}

	c.Writeln("\tmodel.run()")

	return
}

func (c *CCMPyACTR) outputPattern(pattern *actr.Pattern) {
	str := fmt.Sprintf("'%s ", pattern.Chunk.Name)

	for i, slot := range pattern.Slots {
		slotStr := slot.String()

		if slotStr == "nil" {
			str += "None"
		} else {
			str += slot.String()

		}

		if i != len(pattern.Slots)-1 {
			str += " "
		}
	}

	str += "'"

	c.Write(str)
}

func (c *CCMPyACTR) outputMatch(match *actr.Match) {
	var name string
	if match.Buffer != nil {
		name = match.Buffer.GetName()
	} else if match.Memory != nil {
		name = match.Memory.Name
	}

	chunkName := match.Pattern.Chunk.Name
	if actr.IsInternalChunkName(chunkName) {
		if chunkName == "_status" {
			status := match.Pattern.Slots[0]
			c.Write("%s='%s:True'", name, status)
		}
	} else {
		c.Write("%s=", name)
		c.outputPattern(match.Pattern)
	}
}

func (c *CCMPyACTR) outputStatement(s *actr.Statement) {
	if s.Set != nil {
		if s.Set.Slots != nil {
			slotAssignments := []string{}
			for _, slot := range *s.Set.Slots {
				value := convertSetValue(slot.Value)
				slotAssignments = append(slotAssignments, fmt.Sprintf("_%d=%s", slot.SlotIndex, value))
			}
			c.Writeln("\t\t%s.modify(%s)", s.Set.Buffer.GetName(), strings.Join(slotAssignments, ", "))
		} else {
			c.Write("\t\t%s.set(", s.Set.Buffer.GetName())
			c.outputPattern(s.Set.Pattern)
			c.Writeln(")")
		}
	} else if s.Recall != nil {
		c.Write("\t\t%s.request(", s.Recall.Memory.Name)
		c.outputPattern(s.Recall.Pattern)
		c.Writeln(")")
	} else if s.Clear != nil {
		for _, name := range s.Clear.BufferNames {
			c.Writeln("\t\t%s.clear()", name)
		}
	} else if s.Print != nil {
		values := framework.PythonValuesToStrings(s.Print.Values, true)
		c.Writeln("\t\tprint(%s)", strings.Join(values, ", "))
	}
}

func convertSetValue(s *actr.SetValue) string {
	if s.Nil {
		return "None"
	} else if s.Var != nil {
		return *s.Var
	} else if s.Number != nil {
		return *s.Number
	} else if s.Str != nil {
		return "'" + *s.Str + "'"
	}

	return ""
}

// convertGoal strips out the parentheses for output.
func convertGoal(g *actr.Pattern) string {
	goal := g.String()

	goal = strings.Replace(goal, "(", "", 1)
	goal = strings.ReplaceAll(goal, " nil", " None")
	goal = strings.TrimSuffix(goal, " )")

	return goal
}
