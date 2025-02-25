package ccm_pyactr

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kylelemons/godebug/diff"

	"github.com/asmaloney/gactar/framework"
	"github.com/asmaloney/gactar/util/cli"
)

func init() {
	framework.GactarVersion = "test"
	framework.TimeNow = func() time.Time {
		return time.Time{}
	}
}

func TestCodeGeneration(t *testing.T) {
	ctx := &cli.Settings{}

	// Make sure we can find the right Python.
	// Since this is just for testing, we can hardcode it.
	err := cli.SetupPaths("../../env")
	if err != nil {
		t.Fatal(err)
	}

	fw, err := New(ctx)

	if fw == nil {
		fmt.Println(err.Error())
		t.Skip("ccm framework not active")
	}

	// determine input files
	match, err := filepath.Glob("../testdata/*.amod")
	if err != nil {
		t.Fatal(err)
	}

	for _, input := range match {
		name := filepath.Base(input)
		t.Run(name, func(t *testing.T) {
			output := input[:len(input)-len(".amod")] + ".py.golden"
			output = filepath.Join("testdata", output)

			runCodeGenerationTest(t, fw, input, output)
		})
	}
}

func runCodeGenerationTest(t *testing.T, fw framework.Framework, input, output string) { //nolint to avoid Helper info since it doesn't apply
	code, err := framework.GenerateCodeFromFile(fw, input, framework.InitialBuffers{})
	if err != nil {
		t.Error(err)
		return
	}

	expected, err := os.ReadFile(output)
	if err != nil {
		file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0660)
		if err != nil {
			return
		}
		defer file.Close()

		_, err = file.WriteString(string(code))
		if err != nil {
			return
		}

		t.Skip("golden file did not exist, so I created it")
		return
	}

	if !bytes.Equal(code, expected) {
		diffs := diff.Diff(string(expected), string(code))
		t.Errorf("code does not match %s file:\n%s", output, diffs)
	}
}
