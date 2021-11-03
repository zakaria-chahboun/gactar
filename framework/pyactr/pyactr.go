package pyactr

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/asmaloney/gactar/actr"
	"github.com/asmaloney/gactar/framework"
	"github.com/asmaloney/gactar/version"
)

//go:embed pyactr_print.py
var pyactrPrintPython string

type PyACTR struct {
	framework.WriterHelper
	model     *actr.Model
	className string
	tmpPath   string
}

// New simply creates a new PyACTR instance and sets the tmp path.
func New(cli *cli.Context) (p *PyACTR, err error) {

	p = &PyACTR{tmpPath: "tmp"}

	return
}

func (p *PyACTR) Initialize() (err error) {
	_, err = framework.CheckForExecutable("python3")
	if err != nil {
		return
	}

	framework.IdentifyYourself("pyactr", "python3")

	err = framework.PythonCheckForPackage("pyactr")
	if err != nil {
		return
	}

	err = os.MkdirAll(p.tmpPath, os.ModePerm)
	if err != nil {
		return
	}

	return
}

func (p *PyACTR) SetModel(model *actr.Model) (err error) {
	if model.Name == "" {
		err = fmt.Errorf("model is missing name")
		return
	}

	p.model = model
	p.className = fmt.Sprintf("pyactr_%s", p.model.Name)

	return
}

func (p *PyACTR) Run(initialBuffers framework.InitialBuffers) (generatedCode, output []byte, err error) {
	outputFile, err := p.WriteModel(p.tmpPath, initialBuffers)
	if err != nil {
		return
	}

	// run it!
	cmd := exec.Command("python3", outputFile)

	output, err = cmd.CombinedOutput()
	output = removeWarning(output)
	if err != nil {
		err = fmt.Errorf("%s", string(output))
		return
	}

	generatedCode = p.GetContents()

	return
}

func (p *PyACTR) WriteModel(path string, initialBuffers framework.InitialBuffers) (outputFileName string, err error) {
	patterns, err := framework.ParseInitialBuffers(p.model, initialBuffers)
	if err != nil {
		return
	}
	goal := patterns["goal"]

	// If our model has a print statement, then write out our support file
	if p.model.HasPrintStatement() {
		supportFileName := "pyactr_print.py"
		if path != "" {
			supportFileName = fmt.Sprintf("%s/%s", path, supportFileName)
		}

		file, err := os.OpenFile(supportFileName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0660)
		if err != nil {
			return "", err
		}
		defer file.Close()

		file.WriteString(pyactrPrintPython)
	}

	outputFileName = fmt.Sprintf("%s.py", p.className)
	if path != "" {
		outputFileName = fmt.Sprintf("%s/%s", path, outputFileName)
	}

	err = framework.RemoveTempFile(outputFileName)
	if err != nil {
		return "", err
	}

	err = p.InitWriterHelper(outputFileName)
	if err != nil {
		return
	}
	defer p.CloseWriterHelper()

	p.Writeln("# Generated by gactar %s", version.BuildVersion)
	p.Writeln("#           on %s", time.Now().Format("2006-01-02 @ 15:04:05"))
	p.Writeln("#   https://github.com/asmaloney/gactar")
	p.Writeln("")
	p.Writeln("# *** NOTE: This is a generated file. Any changes may be overwritten.")
	p.Writeln("")
	p.Write("# %s\n\n", p.model.Description)

	p.outputAuthors()

	p.Writeln("import pyactr as actr")

	if p.model.HasPrintStatement() {
		// Import gactar's print handling
		p.Writeln("import pyactr_print")
	}

	p.Writeln("")

	memory := p.model.Memory
	additionalInit := []string{}

	if memory.Latency != nil {
		additionalInit = append(additionalInit, fmt.Sprintf("latency_factor=%s", framework.Float64Str(*memory.Latency)))
	}

	if memory.Threshold != nil {
		additionalInit = append(additionalInit, fmt.Sprintf("retrieval_threshold=%s", framework.Float64Str(*memory.Threshold)))
	}

	p.Writeln("%s = actr.ACTRModel(%s)", p.className, strings.Join(additionalInit, ", "))

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
		p.Writeln("actr.chunktype('%s', '%s')", chunk.Name, strings.Join(chunk.SlotNames, ", "))
	}
	p.Writeln("")

	p.Writeln("dm = %s.decmem", p.className)
	p.Writeln("goal = %s.set_goal('goal')", p.className)
	p.Writeln("")

	imaginal := p.model.GetImaginal()
	if imaginal != nil {
		p.Writeln(`imaginal = %s.set_goal(name="imaginal", delay=%s)`, p.className, framework.Float64Str(imaginal.Delay))
		p.Writeln("")
	}

	// initialize
	for _, init := range p.model.Initializers {
		initializer := "dm"
		if init.Buffer.GetBufferName() != "retrieval" {
			initializer = init.Buffer.GetBufferName()

			// allow the user-set goal to override the initializer
			if initializer == "goal" && (goal != nil) {
				continue
			}
		}
		p.Writeln("# amod line %d", init.AMODLineNumber)
		p.Writeln("%s.add(actr.chunkstring(string='''", initializer)
		p.outputPattern(init.Pattern, 1)
		p.Writeln("'''))")
	}

	// Add user-set goal if any
	if goal != nil {
		p.Writeln("goal.add(actr.chunkstring(string='''")
		p.outputPattern(goal, 1)
		p.Writeln("'''))")
	}

	p.Writeln("")

	// productions
	for _, production := range p.model.Productions {
		if production.Description != nil {
			p.Writeln("# %s", *production.Description)
		}

		p.Writeln("# amod line %d", production.AMODLineNumber)

		p.Writeln("%s.productionstring(name='%s', string='''", p.className, production.Name)
		for _, match := range production.Matches {
			p.outputMatch(match)
		}

		p.Writeln("\t==>")

		numPrintStatements := 0
		warnings := []string{}
		if production.DoStatements != nil {
			for _, statement := range production.DoStatements {
				if statement.Print != nil {
					numPrintStatements++
					if numPrintStatements > 1 {
						warning := fmt.Sprintf("Warning: ('%s') pyactr currently only supports one print statement per production", production.Name)
						warnings = append(warnings, warning)
						continue
					}
				}

				p.outputStatement(production, statement)
			}
		}

		p.Write("''')\n\n")

		if len(warnings) > 0 {
			for _, warning := range warnings {
				p.Writeln(`print("%s")`, warning)
			}

			p.Writeln("")
		}
	}

	p.Writeln("")

	// ...add our code to run
	p.Writeln("# Main")
	p.Writeln("if __name__ == '__main__':")
	p.Writeln("\tsim = %s.simulation()", p.className)
	p.Writeln("\tsim.run()")
	// TODO: Add some intelligent output when logging level is info or detail
	p.Writeln("\tif goal.test_buffer('full') is True:")
	p.Writeln("\t\tprint('final goal: ' + str(goal.pop()))")

	return
}

func (p *PyACTR) outputAuthors() {
	if len(p.model.Authors) == 0 {
		return
	}

	p.Writeln("# Authors:")

	for _, author := range p.model.Authors {
		p.Write("#\t%s\n", author)
	}

	p.Writeln("")
}

func (p *PyACTR) outputPattern(pattern *actr.Pattern, tabs int) {
	tabbedItems := framework.KeyValueList{}
	tabbedItems.Add("isa", pattern.Chunk.Name)

	for i, slot := range pattern.Slots {
		slotName := pattern.Chunk.SlotNames[i]
		addPatternSlot(&tabbedItems, slotName, slot)
	}

	p.TabWrite(tabs, tabbedItems)
}

func (p *PyACTR) outputMatch(match *actr.Match) {
	bufferName := match.Buffer.GetBufferName()
	chunkName := match.Pattern.Chunk.Name

	if actr.IsInternalChunkName(chunkName) {
		if chunkName == "_status" {
			status := match.Pattern.Slots[0]
			p.Writeln("\t?%s>", bufferName)

			// Table 2.1 page 24 of pyactr book
			if status.String() == "full" || status.String() == "empty" {
				p.Writeln("\t\tbuffer %s", status)
			} else {
				p.Writeln("\t\tstate %s", status)
			}
		}
	} else {
		p.Writeln("\t=%s>", bufferName)
		p.outputPattern(match.Pattern, 2)
	}
}

func addPatternSlot(tabbedItems *framework.KeyValueList, slotName string, patternSlot *actr.PatternSlot) {
	for _, item := range patternSlot.Items {
		var value string
		if item.Negated {
			value = "~"
		}

		if item.Nil {
			value += "nil"
		} else if item.ID != nil {
			value += fmt.Sprintf(`"%s"`, *item.ID)
		} else if item.Num != nil {
			value += *item.Num
		} else if item.Var != nil {
			if *item.Var == "?" {
				return
			}

			value += "="
			value += strings.TrimPrefix(*item.Var, "?")
		}

		tabbedItems.Add(slotName, value)
	}
}

func (p *PyACTR) outputStatement(production *actr.Production, s *actr.Statement) {
	if s.Set != nil {
		buffer := s.Set.Buffer
		bufferName := buffer.GetBufferName()

		p.Write("\t=%s>\n", bufferName)

		if s.Set.Slots != nil {
			tabbedItems := framework.KeyValueList{}
			tabbedItems.Add("isa", s.Set.Chunk.Name)

			for _, slot := range *s.Set.Slots {
				slotName := slot.Name

				if slot.Value.Nil {
					tabbedItems.Add(slotName, "nil")
				} else if slot.Value.Var != nil {
					tabbedItems.Add(slotName, fmt.Sprintf("=%s", *slot.Value.Var))
				} else if slot.Value.Number != nil {
					tabbedItems.Add(slotName, *slot.Value.Number)
				} else if slot.Value.Str != nil {
					tabbedItems.Add(slotName, fmt.Sprintf(`"%s"`, *slot.Value.Str))
				}
			}
			p.TabWrite(2, tabbedItems)
		} else if s.Set.Pattern != nil {
			p.outputPattern(s.Set.Pattern, 2)
		}
	} else if s.Recall != nil {
		p.Writeln("\t~retrieval>")
		p.Writeln("\t+retrieval>")
		p.outputPattern(s.Recall.Pattern, 2)
	} else if s.Print != nil {
		// Using "goal" here is arbitrary because of the way we monkey patch the python code.
		// Our "print_text" statement handles its own formatting and lookup.
		p.Writeln("\t!goal>")

		str := make([]string, len(*s.Print.Values))

		for index, val := range *s.Print.Values {
			if val.Var != nil {
				varIndex := production.VarIndexMap[*val.Var]
				str[index] = fmt.Sprintf("%s.%s", varIndex.Buffer.GetBufferName(), varIndex.SlotName)
			} else if val.Str != nil {
				str[index] = fmt.Sprintf("'%s'", *val.Str)
			} else if val.Number != nil {
				str[index] = *val.Number
			}
		}

		p.Writeln("\t\tprint_text \"%s\"", strings.Join(str, ", "))
	} else if s.Clear != nil {
		for _, name := range s.Clear.BufferNames {
			p.Writeln("\t~%s>", name)
		}
	}
}

// removeWarning will remove the long warning whenever pyactr is run without tkinter.
func removeWarning(text []byte) []byte {
	str := string(text)

	r := regexp.MustCompile(`(?s).+warnings.warn\("Simulation GUI is set to False."\)(.+)`)
	matches := r.FindAllStringSubmatch(str, -1)
	if len(matches) == 1 {
		str = strings.TrimSpace(matches[0][1])
	}

	return []byte(str)
}
