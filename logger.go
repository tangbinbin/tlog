package tlog

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/Shopify/sarama"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"sync"
	"time"
)

var l *Logger
var mu sync.Mutex

type Logger struct {
	fileSize int
	fileNum  int
	fileName string
	s        int
	debug    bool
	toKafka  bool
	level    LEVEL
	dir      string
	topic    string
	kafka    []string
	ch       chan *Atom
	bytePool *sync.Pool
	f        *os.File
	w        *bufio.Writer
	producer sarama.AsyncProducer
}

type Atom struct {
	line   int
	file   string
	format string
	level  LEVEL
	args   []interface{}
}

func newLogger(config Config) {
	l = &Logger{
		dir:      config.Dir,
		kafka:    config.Kafka,
		topic:    config.Topic,
		fileSize: config.FileSize * 1024 * 1024,
		fileNum:  config.FileNum,
		fileName: path.Join(config.Dir, config.FileName+".log"),
		debug:    config.Debug,
		level:    getLevel(config.Level),
		bytePool: &sync.Pool{New: func() interface{} { return new(bytes.Buffer) }},
		ch:       make(chan *Atom, 1024),
	}
	if l.debug {
		return
	}
	if len(l.kafka) != 0 && l.topic != "" {
		l.toKafka = true
	}

	os.MkdirAll(l.dir, 0755)
	l.f, _ = os.OpenFile(l.fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	fileInfo, _ := os.Stat(l.fileName)
	l.s = int(fileInfo.Size())
	l.w = bufio.NewWriterSize(l.f, 1024*1024)

	if l.toKafka {
		kafkaConfig := sarama.NewConfig()
		kafkaConfig.Producer.Compression = sarama.CompressionSnappy
		kafkaConfig.Producer.Return.Errors = false
		kafkaConfig.Producer.Flush.Messages = 200
		kafkaConfig.Producer.Flush.Bytes = 100000
		kafkaConfig.Producer.Flush.Frequency = time.Second
		kafkaConfig.Producer.RequiredAcks = sarama.NoResponse
		l.producer, _ = sarama.NewAsyncProducer(l.kafka, kafkaConfig)
	}
}

func (l *Logger) start() {
	for {
		a := <-l.ch
		if a == nil {
			if _, err := os.Stat(l.fileName); err != nil && os.IsNotExist(err) {
				l.f.Close()
				l.f, _ = os.OpenFile(l.fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				l.w.Reset(l.f)
				l.s = 0
			}
			l.w.Flush()
			continue
		}
		if l.s > l.fileSize {
			l.w.Flush()
			l.f.Close()
			os.Rename(l.fileName, l.logname())
			l.f, _ = os.OpenFile(l.fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			l.w.Reset(l.f)
			l.s = 0
			l.rm()
		}
		if l.toKafka {
			n, b := l.format(a)
			l.w.Write(b)
			l.s += n
			l.producer.Input() <- &sarama.ProducerMessage{
				Topic: l.topic,
				Value: sarama.ByteEncoder(b),
			}
		} else {
			l.write(a)
		}

	}
}

func (l *Logger) run() {
	if l.debug {
		return
	}
	go l.flush()
	go l.start()
}

func (l *Logger) stop() {
	if l != nil && l.w != nil {
		l.w.Flush()
	}
}

func (l *Logger) write(a *Atom) {
	now := time.Now()
	t := now.Nanosecond() / 1000
	_, month, day := now.Date()
	hour, minute, second := now.Clock()
	n, _ := l.w.Write([]byte{
		byte(month/10) + 48, byte(month%10) + 48, '-', byte(day/10) + 48, byte(day%10) + 48, ' ',
		byte(hour/10) + 48, byte(hour%10) + 48, ':', byte(minute/10) + 48, byte(minute%10) + 48, ' ',
		byte(second/10) + 48, byte(second%10) + 48, '.',
		byte((t%1000000)/100000) + 48, byte((t%100000)/10000) + 48, byte((t%10000)/1000) + 48,
		byte((t%1000)/100) + 48, byte((t%100)/10) + 48, byte(t%10) + 48, ' ',
	})
	l.s += n
	switch a.level {
	case DEBUG:
		w.WriteString("DEBUG ")
		l.s += 6
	case INFO:
		w.WriteString("INFO ")
		l.s += 5
	case WARNING:
		w.WriteString("WARNING ")
		l.s += 8
	case ERROR:
		w.WriteString("ERROR ")
		l.s += 6
	case FATAL:
		w.WriteString("FATAL ")
		l.s += 6
	default:
	}
	n, _ = w.WriteString(a.file)
	w.Write([]byte{':', byte((a.line%10000)/1000) + 48, byte((a.line%1000)/100) + 48,
		byte((a.line%100)/10) + 48, byte(a.line%10) + 48, ' '})
	l.s += n
	l.s += 6
	if len(a.format) == 0 {
		l.w.Write(' ')
		l.s++
		n, _ = fmt.Fprint(w, a.args...)
		l.s += n
	} else {
		n, _ = fmt.Fprintf(w, a.format, a.args...)
		l.s += n
	}
	w.WriteByte(10)
	l.s++
}

func (l *Logger) format(a *Atom) (int, []byte) {
	w := l.bytePool.Get().(*bytes.Buffer)
	defer func() {
		w.Reset()
		l.bytePool.Put(w)
	}()
	now := time.Now()
	t := now.Nanosecond() / 1000
	_, month, day := now.Date()
	hour, minute, second := now.Clock()
	w.Write([]byte{byte(month/10) + 48, byte(month%10) + 48, '-',
		byte(day/10) + 48, byte(day%10) + 48, ' ',
		byte(hour/10) + 48, byte(hour%10) + 48, ':',
		byte(minute/10) + 48, byte(minute%10) + 48, ':',
		byte(second/10) + 48, byte(second%10) + 48, '.',
		byte((t%1000000)/100000) + 48, byte((t%100000)/10000) + 48, byte((t%10000)/1000) + 48,
		byte((t%1000)/100) + 48, byte((t%100)/10) + 48, byte(t%10) + 48, ' ',
	})
	switch a.level {
	case DEBUG:
		w.WriteString("DEBUG ")
	case INFO:
		w.WriteString("INFO ")
	case WARNING:
		w.WriteString("WARNING ")
	case ERROR:
		w.WriteString("ERROR ")
	case FATAL:
		w.WriteString("FATAL ")
	default:
	}
	w.WriteString(a.file)
	w.Write([]byte{':', byte((a.line%10000)/1000) + 48, byte((a.line%1000)/100) + 48,
		byte((a.line%100)/10) + 48, byte(a.line%10) + 48, ' '})
	if len(a.format) == 0 {
		w.WriteByte(' ')
		fmt.Fprint(w, a.args...)
	} else {
		fmt.Fprintf(w, a.format, a.args...)
	}
	w.WriteByte(10)
	len := w.Len()
	data := make([]byte, len)
	copy(data, w.Bytes())
	return len, data
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
	for range time.NewTicker(time.Second).C {
		l.ch <- nil
	}
}

func (l *Logger) logname() string {
	t := fmt.Sprintf("%s", time.Now())[:19]
	tt := strings.Replace(
		strings.Replace(
			strings.Replace(t, "-", "", -1),
			" ", "", -1),
		":", "", -1)
	return fmt.Sprintf("%s.%s", l.fileName, tt)
}

func (l *Logger) genTime() string {
	return fmt.Sprintf("%s", time.Now())[:26]
}

func (l *Logger) p(level LEVEL, args ...interface{}) {
	file, line := l.getFileNameAndLine()
	if l == nil || l.debug {
		mu.Lock()
		defer mu.Unlock()
		fmt.Printf("%s %s %s:%d ", l.genTime(), levelText[level], file, line)
		fmt.Println(args...)
		return
	}
	if level >= l.level {
		l.ch <- &Atom{file: file, line: line, level: level, args: args}
	}
}

func (l *Logger) pf(level LEVEL, format string, args ...interface{}) {
	file, line := l.getFileNameAndLine()
	if l == nil || l.debug {
		mu.Lock()
		defer mu.Unlock()
		fmt.Printf("%s %s %s:%d ", l.genTime(), levelText[level], file, line)
		fmt.Printf(format, args...)
		fmt.Println()
		return
	}
	if level >= l.level {
		l.ch <- &Atom{file: file, line: line, format: format, level: level, args: args}
	}
}

func (l *Logger) getFileNameAndLine() (string, int) {
	_, file, line, ok := runtime.Caller(3)
	if !ok {
		return "???", 1
	}
	dirs := strings.Split(file, "/")
	if len(dirs) >= 2 {
		return dirs[len(dirs)-2] + "/" + dirs[len(dirs)-1], line
	}
	return file, line
}
