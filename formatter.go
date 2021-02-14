package formatter

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	isatty "github.com/mattn/go-isatty"
	"github.com/sirupsen/logrus"
)

const (
	defaultTimestampFormat = "2006-01-02 15:04:05 MST"

	faint  = 2
	red    = 31
	yellow = 33
	blue   = 36
	gray   = 37
)

type TextFormatter struct {
	TimestampFormat  string
	DisableTimestamp bool

	ForceColors   bool
	DisableColors bool

	ForceQuote   bool
	DisableQuote bool

	TruncateLevelText bool
	PadLevelText      bool

	DisableSorting bool

	SortingFunc func([]string)

	CallerPrettyfier func(*runtime.Frame) (function string, file string)

	terminalInitOnce sync.Once

	isTerminal         bool
	levelTextMaxLength int
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd())
}

func (f *TextFormatter) init(entry *logrus.Entry) {
	if entry.Logger != nil {
		f.isTerminal = isTerminal(entry.Logger.Out)
	}
	for _, level := range logrus.AllLevels {
		levelTextLength := utf8.RuneCount([]byte(level.String()))
		if levelTextLength > f.levelTextMaxLength {
			f.levelTextMaxLength = levelTextLength
		}
	}
}

func (f *TextFormatter) isColored() bool {
	isColored := f.ForceColors || (f.isTerminal && (runtime.GOOS != "windows"))

	return isColored && !f.DisableColors
}

func colorPrint(text string, color int) string {
	if color > 0 {
		return fmt.Sprintf("\x1b[%dm%s\x1b[0m", color, text)
	}
	return text
}

func (f *TextFormatter) needsQuoting(text string) bool {
	if f.ForceQuote {
		return true
	}
	if f.DisableQuote {
		return false
	}
	for _, ch := range text {
		if !((ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') ||
			ch == '-' || ch == '.' || ch == '_' || ch == '/' || ch == '@' || ch == '^' || ch == '+') {
			return true
		}
	}
	return false
}

func (f *TextFormatter) appendValue(b *bytes.Buffer, value interface{}) {
	stringVal, ok := value.(string)
	if !ok {
		stringVal = fmt.Sprint(value)
	}

	if !f.needsQuoting(stringVal) {
		b.WriteString(stringVal)
	} else {
		b.WriteString(fmt.Sprintf("%q", stringVal))
	}
}

func (f *TextFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	data := make(logrus.Fields)
	for k, v := range entry.Data {
		data[k] = v
	}

	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}

	if !f.DisableSorting {
		if f.SortingFunc == nil {
			sort.Strings(keys)
		} else {
			f.SortingFunc(keys)
		}
	}

	var b *bytes.Buffer
	if entry.Buffer != nil {
		b = entry.Buffer
	} else {
		b = &bytes.Buffer{}
	}

	f.terminalInitOnce.Do(func() { f.init(entry) })

	timestampFormat := f.TimestampFormat
	if timestampFormat == "" {
		timestampFormat = defaultTimestampFormat
	}

	timestamp := entry.Time.Format(timestampFormat)

	levelColor := -1
	separator := " :: "

	if f.isColored() {
		switch entry.Level {
		case logrus.DebugLevel, logrus.TraceLevel:
			levelColor = gray
		case logrus.WarnLevel:
			levelColor = yellow
		case logrus.ErrorLevel, logrus.FatalLevel, logrus.PanicLevel:
			levelColor = red
		default:
			levelColor = blue
		}

		timestamp = colorPrint(timestamp, faint)
		separator = " "
	}

	levelText := strings.ToUpper(entry.Level.String())

	if f.TruncateLevelText && !f.PadLevelText {
		levelText = levelText[0:4]
	}
	if f.PadLevelText {
		formatString := "%-" + strconv.Itoa(f.levelTextMaxLength) + "s"
		levelText = fmt.Sprintf(formatString, levelText)
	}

	entry.Message = strings.TrimSuffix(entry.Message, "\n")

	caller := ""
	if entry.HasCaller() {
		var (
			funcVal = fmt.Sprintf("%s:%d", entry.Caller.Function, entry.Caller.Line)
			fileVal string
		)

		if f.CallerPrettyfier != nil {
			funcVal, fileVal = f.CallerPrettyfier(entry.Caller)
		}

		if fileVal == "" {
			caller = funcVal
		} else if funcVal == "" {
			caller = fileVal
		} else {
			caller = fileVal + " " + funcVal
		}

		caller = " (" + caller + ")"
	}

	switch {
	case f.DisableTimestamp:
		colorSection := colorPrint(fmt.Sprintf("%s%s", levelText, caller), levelColor)
		template := fmt.Sprintf("%%s%s%%-44s ", separator)
		fmt.Fprintf(b, template, colorSection, entry.Message)
	default:
		colorSection := colorPrint(fmt.Sprintf("%s%s", levelText, caller), levelColor)
		template := fmt.Sprintf("%%s%s%%s%[1]s%%-44s ", separator)
		fmt.Fprintf(b, template, timestamp, colorSection, entry.Message)
	}
	for _, k := range keys {
		v := data[k]
		fmt.Fprintf(b, " %s=", colorPrint(k, levelColor))
		f.appendValue(b, v)
	}

	b.WriteByte('\n')
	return b.Bytes(), nil
}
