package tlog

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"time"
)

type Config struct {
	Dir      string `toml:"dir"`
	FileSize int    `toml:"filesize"`
	FileNum  int    `toml:"filenum"`
	Debug    bool   `toml:"debug"`
}

var l *Logger

type Logger struct {
	dir      string
	fileSize int
	fileNum  int
	debug    bool
	ch       chan *Atom
	s        int
	f        *os.File
	w        *bufio.Writer
}

type Atom struct {
	file   string
	line   int
	format string
	args   []interface{}
}

//API
func Init(config Config) {
	l = &Logger{
		dir:      config.Dir,
		fileSize: config.FileSize * 1024 * 1024,
		fileNum:  config.FileNum,
		debug:    config.Debug,
		ch:       make(chan *Atom, 1024),
	}
	if l.debug {
		return
	}
	os.MkdirAll(l.dir, 0755)
	l.f, _ = os.OpenFile(filename(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	fileInfo, _ := os.Stat(filename())
	l.s = int(fileInfo.Size())
	l.w = bufio.NewWriterSize(l.f, 4*1024*1024)
	go l.flush()
	go l.start()
}

func Close() {
	if l.w != nil {
		l.w.Flush()
	}
}

func Infof(format string, args ...interface{}) {
	_, file, line, ok := runtime.Caller(1)
	if !ok {
		file = "???"
		line = 1
	} else {
		file = path.Base(file)
	}
	if l == nil || l.debug {
		fmt.Printf("%s %s:%d ", genTime(), file, line)
		fmt.Printf(format, args...)
		fmt.Println()
		return
	}
	l.ch <- &Atom{
		file:   file,
		line:   line,
		format: format,
		args:   args,
	}
}

func Info(args ...interface{}) {
	_, file, line, ok := runtime.Caller(1)
	if !ok {
		file = "???"
		line = 1
	} else {
		file = path.Base(file)
	}
	if l == nil || l.debug {
		fmt.Printf("%s %s:%d ", genTime(), file, line)
		fmt.Println(args...)
		return
	}
	l.ch <- &Atom{
		file: file,
		line: line,
		args: args,
	}
}

//Internal
func (l *Logger) start() {
	for {
		a := <-l.ch
		if a == nil {
			if _, err := os.Stat(filename()); err != nil && os.IsNotExist(err) {
				l.f.Close()
				l.f, _ = os.OpenFile(filename(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				l.w.Reset(l.f)
				l.s = 0
			}
			l.w.Flush()
			continue
		}
		if l.s > l.fileSize {
			l.w.Flush()
			l.f.Close()
			os.Rename(filename(), logname())
			l.f, _ = os.OpenFile(filename(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			l.w.Reset(l.f)
			l.s = 0
			l.rm()
		}
		if l.w.Buffered() > 1024*1024 {
			l.w.Flush()
		}
		now := time.Now()
		t := now.Nanosecond() / 1000
		year, month, day := now.Date()
		hour, minute, second := now.Clock()
		l.w.Write([]byte{
			50,
			48,
			49,
			byte(year%10) + 48,
			45,
			byte(month/10) + 48,
			byte(month%10) + 48,
			45,
			byte(day/10) + 48,
			byte(day%10) + 48,
			32,
			byte(hour/10) + 48,
			byte(hour%10) + 48,
			58,
			byte(minute/10) + 48,
			byte(minute%10) + 48,
			58,
			byte(second/10) + 48,
			byte(second%10) + 48,
			46,
			byte((t%1000000)/100000) + 48,
			byte((t%100000)/10000) + 48,
			byte((t%10000)/1000) + 48,
			byte((t%1000)/100) + 48,
			byte((t%100)/10) + 48,
			byte(t%10) + 48,
			32,
		})
		l.w.WriteString(a.file)
		l.s += len(a.file)
		l.w.Write([]byte{
			58,
			byte((a.line%10000)/1000) + 48,
			byte((a.line%1000)/100) + 48,
			byte((a.line%100)/10) + 48,
			byte(a.line%10) + 48,
			32})
		n := 0
		if a.format == "" {
			n, _ = fmt.Fprint(l.w, a.args...)
		} else {
			n, _ = fmt.Fprintf(l.w, a.format, a.args...)
		}
		l.w.WriteByte(10)
		l.s += n
		l.s += 34
	}
}

func (l *Logger) rm() {
	if out, err := exec.Command("ls", l.dir).Output(); err == nil {
		files := bytes.Split(out, []byte("\n"))
		totol, idx := len(files)-1, 0
		for i := totol; i >= 0; i-- {
			file := string(files[i])
			if strings.HasPrefix(file, "INFO.log") {
				idx++
				if idx > l.fileNum {
					exec.Command("rm", path.Join(l.dir, file)).Run()
				}
			}
		}
	}
}

func (l *Logger) flush() {
	for range time.NewTicker(3 * time.Second).C {
		l.ch <- nil
	}
}

func filename() string {
	return path.Join(l.dir, "INFO.log")
}

func logname() string {
	t := fmt.Sprintf("%s", time.Now())[:19]
	tt := strings.Replace(
		strings.Replace(
			strings.Replace(t, "-", "", -1),
			" ", "", -1),
		":", "", -1)
	return fmt.Sprintf("%s.%s", filename(), tt)
}

func genTime() string {
	return fmt.Sprintf("%s", time.Now())[:26]
}
