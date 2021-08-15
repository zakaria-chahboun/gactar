package framework

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

type WriterHelper struct {
	File      *os.File
	TabWriter *tabwriter.Writer
}

// KeyValueList is used to format output nicely with tabs using tabwriter.
type KeyValueList struct {
	list []keyValue
}

type keyValue struct {
	key   string
	value string
}

func (w *WriterHelper) InitWriterHelper(outputFileName string) (err error) {
	w.File, err = os.Create(outputFileName)
	if err != nil {
		return
	}

	w.TabWriter = tabwriter.NewWriter(w.File, 0, 4, 1, '\t', 0)

	return
}

func (w WriterHelper) Write(e string, a ...interface{}) {
	str := fmt.Sprintf(e, a...)
	w.File.WriteString(str)
}

func (w WriterHelper) Writeln(e string, a ...interface{}) {
	w.Write(e+"\n", a...)
}

func (w WriterHelper) TabWrite(level int, list KeyValueList) {
	tabs := strings.Repeat("\t", level)
	for _, item := range list.list {
		fmt.Fprintf(w.TabWriter, "%s%s\t%s\n", tabs, item.key, item.value)
	}

	w.TabWriter.Flush()
}

func (l *KeyValueList) AddArrays(keys, values []string) (err error) {
	if len(keys) != len(values) {
		err = fmt.Errorf("(internal error) expecting arrays of the same length")
		return
	}

	for i, key := range keys {
		l.Add(key, values[i])
	}

	return
}

func (l *KeyValueList) Add(key, value string) {
	l.list = append(l.list, keyValue{
		key:   key,
		value: value,
	})
}
