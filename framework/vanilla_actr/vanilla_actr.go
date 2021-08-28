package vanilla_actr

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v2"
	"gitlab.com/asmaloney/gactar/actr"
	"gitlab.com/asmaloney/gactar/amod"
	"gitlab.com/asmaloney/gactar/framework"
)

type VanillaACTR struct {
	framework.WriterHelper
	model     *actr.Model
	modelName string
	tmpPath   string
	envPath   string
}

// New simply creates a new VanillaACTR instance and sets the tmp path.
func New(cli *cli.Context) (v *VanillaACTR, err error) {

	v = &VanillaACTR{
		tmpPath: "tmp",
		envPath: cli.String("env"),
	}

	return
}

func (v *VanillaACTR) Initialize() (err error) {
	_, err = framework.CheckForExecutable("sbcl")
	if err != nil {
		return
	}

	framework.IdentifyYourself("vanilla", "sbcl")

	return
}

func (v *VanillaACTR) SetModel(model *actr.Model) (err error) {
	if model.Name == "" {
		err = fmt.Errorf("model is missing name")
		return
	}

	v.model = model
	v.modelName = fmt.Sprintf("gactar_vactr_%s", strings.Title(v.model.Name))

	return
}

func (v *VanillaACTR) Run(initialGoal string) (output []byte, err error) {
	modelFile, err := v.WriteModel(v.tmpPath, initialGoal)
	if err != nil {
		return
	}

	runFile, err := v.createRunFile(modelFile)
	if err != nil {
		return
	}

	// run it!
	command := fmt.Sprintf("./%s", runFile)
	cmd := exec.Command(command)

	// set SBCL_HOME so compiler works
	sbclPath := fmt.Sprintf("%s/lib/sbcl", v.envPath)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("SBCL_HOME=%s", sbclPath))

	output, err = cmd.CombinedOutput()
	output = removePreamble(output)
	if err != nil {
		err = fmt.Errorf("%s", string(output))
		return
	}

	return
}

func (v *VanillaACTR) WriteModel(path, initialGoal string) (outputFile string, err error) {
	goal, err := amod.ParseChunk(v.model, initialGoal)
	if err != nil {
		err = fmt.Errorf("error in initial goal - %s", err)
		return
	}

	outputFile = fmt.Sprintf("%s.lisp", v.modelName)
	if path != "" {
		outputFile = fmt.Sprintf("%s/%s", path, outputFile)
	}

	err = v.InitWriterHelper(outputFile)
	if err != nil {
		return
	}
	defer v.CloseWriterHelper()

	v.Writeln(";;; This file is generated by gactar %s", time.Now().Format("2006-01-02 15:04:05"))
	v.Writeln(";;; https://github.com/asmaloney/gactar")
	v.Writeln("")
	v.Writeln(";;; *** This is a generated file. Any changes may be overwritten.")
	v.Writeln("")
	v.Write(";;; %s\n\n", v.model.Description)

	v.Write("(clear-all)\n\n")

	v.Writeln("(define-model %s\n", v.modelName)
	v.Writeln("(sgp :esc t :lf .05)\n")

	// chunks
	for _, chunk := range v.model.Chunks {
		if chunk.IsInternal() {
			continue
		}

		v.Writeln("(chunk-type %s %s)", chunk.Name, strings.Join(chunk.SlotNames, " "))
	}
	v.Writeln("")

	v.Writeln("(add-dm")
	for i, init := range v.model.Initializers {
		v.Writeln(" (fact_%d", i)
		v.outputPattern(init.Pattern, 1, true)
		v.Writeln(" )")
	}

	// with vanilla act-r, the goal is included with the initializations
	if goal != nil {
		v.Writeln(" (goal")
		v.outputPattern(goal, 1, true)
		v.Writeln(" )")
	}

	v.Writeln(")\n")

	// productions
	for _, production := range v.model.Productions {
		if production.Description != nil {
			v.Writeln(";; %s", *production.Description)
		}

		v.Writeln("(P %s", production.Name)
		for _, match := range production.Matches {
			v.outputMatch(match)
		}

		v.Writeln("\t==>")

		if production.DoStatements != nil {
			for _, statement := range production.DoStatements {
				v.outputStatement(statement)
			}
		}

		v.Writeln(")\n")
	}

	v.Writeln("(goal-focus goal)")

	v.Writeln(")")

	return
}

func (v *VanillaACTR) writeSlot(slot, value string) {
	intValue, conversionErr := strconv.Atoi(value)
	if conversionErr == nil {
		v.Write(" %s %d", slot, intValue)
	} else {
		v.Write(` %s "%s"`, slot, value)
	}
}

func (v *VanillaACTR) outputPattern(pattern *actr.Pattern, tabs int, includeISA bool) {
	tabbedItems := framework.KeyValueList{}

	if includeISA {
		tabbedItems.Add("ISA", pattern.Chunk.Name)
	}

	for i, slot := range pattern.Slots {
		slotName := pattern.Chunk.SlotNames[i]
		addPatternSlot(&tabbedItems, slotName, slot)
	}

	v.TabWrite(tabs, tabbedItems)
}

func (v *VanillaACTR) outputMatch(match *actr.Match) {
	if match.Buffer != nil {
		bufferName := match.Buffer.GetName()
		chunkName := match.Pattern.Chunk.Name

		if actr.IsInternalChunkName(chunkName) {
			if chunkName == "_status" {
				status := match.Pattern.Slots[0]
				v.Writeln("\t?%s>", bufferName)
				v.Writeln("\t\tbuffer %s", status)
			}
		} else {
			v.Writeln("\t=%s>", bufferName)
			v.outputPattern(match.Pattern, 2, true)
		}
	} else if match.Memory != nil {
		text := "retrieval"

		chunkName := match.Pattern.Chunk.Name
		if actr.IsInternalChunkName(chunkName) {
			if chunkName == "_status" {
				status := match.Pattern.Slots[0]
				v.Writeln("\t?%s>", text)
				v.Writeln("\t\tstate %s", status)
			}
		} else {
			v.Writeln("\t=%s>", text)
			v.Writeln("\t\tISA\t%s", chunkName)
		}
	}
}

func addPatternSlot(tabbedItems *framework.KeyValueList, slotName string, patternSlot *actr.PatternSlot) {
	for _, item := range patternSlot.Items {
		value := ""
		slot := ""

		if item.Nil {
			value = "nil"
		} else if item.ID != nil {
			value = fmt.Sprintf(`"%s"`, *item.ID)
		} else if item.Num != nil {
			value = *item.Num
		} else if item.Var != nil {
			if *item.Var == "?" {
				return
			}

			if item.Negated {
				slot = "- "
			}

			varName := strings.TrimPrefix(*item.Var, "?")
			value = fmt.Sprintf("=%s", varName)
		}

		slot += slotName

		tabbedItems.Add(slot, value)
	}
}

func (v *VanillaACTR) outputStatement(s *actr.Statement) {
	if s.Set != nil {
		buffer := s.Set.Buffer

		v.Writeln("\t=%s>", buffer.GetName())

		if s.Set.Slots != nil {
			tabbedItems := framework.KeyValueList{}
			tabbedItems.Add("ISA", s.Set.Chunk.Name)

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
			v.TabWrite(2, tabbedItems)
		} else if s.Set.Pattern != nil {
			v.outputPattern(s.Set.Pattern, 2, false)
		}
	} else if s.Recall != nil {
		v.Writeln("\t+retrieval>")

		v.outputPattern(s.Recall.Pattern, 2, true)
	} else if s.Print != nil {
		values := valuesToStrings(s.Print.Values)
		v.Write("\t!output!\t(%s)\n", strings.Join(values, " "))
	} else if s.Clear != nil {
		for _, name := range s.Clear.BufferNames {
			v.Writeln("\t-%s>", name)
		}
	}
}

func valuesToStrings(values *[]*actr.Value) []string {
	str := make([]string, len(*values))
	for i, v := range *values {
		if v.Var != nil {
			varName := strings.TrimPrefix(*v.Var, "?")
			str[i] = fmt.Sprintf("=%s", varName)
		} else if v.Str != nil {
			// quote the string to preserve case and allow non-alphanumeric characters
			str[i] = fmt.Sprintf(`"%s"`, *v.Str)
		} else if v.Number != nil {
			str[i] = *v.Number
		}
		// v.ID should not be possible because of validation
	}

	return str
}

// createRunFile creates a lisp program to load ACTR and our model and then run them.
func (v *VanillaACTR) createRunFile(modelFile string) (outputFile string, err error) {
	outputFile = fmt.Sprintf("%s_run.lisp", v.modelName)
	if v.tmpPath != "" {
		outputFile = fmt.Sprintf("%s/%s", v.tmpPath, outputFile)
	}

	err = v.InitWriterHelper(outputFile)
	if err != nil {
		return
	}
	defer v.CloseWriterHelper()

	v.Writeln("#!%s/bin/sbcl --script", v.envPath)
	v.Writeln(`(load "%s/actr/load-single-threaded-act-r.lisp")`, v.envPath)
	v.Writeln(`(load "%s")`, modelFile)
	v.Writeln(`(run 10.0)`)

	return
}

// removePreamble will remove the long preamble whenever ACT-R is loaded.
func removePreamble(text []byte) []byte {
	str := string(text)

	r := regexp.MustCompile(`(?s).+######### This is a single threaded build #########(.+)`)
	matches := r.FindAllStringSubmatch(str, -1)
	if len(matches) == 1 {
		str = strings.TrimSpace(matches[0][1])
	}

	return []byte(str)
}
