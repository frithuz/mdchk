package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// event is one go test -json record; see go doc test2json.
type event struct {
	Action     string
	Package    string
	Test       string
	Output     string
	ImportPath string
}

// report counts the outcomes seen in a go test -json stream.
type report struct {
	passed, failed, skipped  int
	packages, failedPackages int
}

// test names one test, or a package's own output when name is empty.
type test struct{ pkg, name string }

// stream holds output until its test, package or build has an outcome.
type stream struct {
	output io.Writer
	report report
	held   map[test]*strings.Builder
	// started keeps each package's tests in order, so a package that ends
	// with tests still running reports them as they began.
	started map[string][]string
	builds  map[string]*strings.Builder
}

// filter reads a go test -json stream and writes only what explains a
// failure: the output of each failing test and subtest, the output of each
// failing package and its unfinished tests, failed build output and any line
// that is not a JSON event. Passing and skipped tests write nothing.
func filter(events io.Reader, output io.Writer) (report, error) {
	s := &stream{
		output:  output,
		held:    map[test]*strings.Builder{},
		started: map[string][]string{},
		builds:  map[string]*strings.Builder{},
	}
	reader := bufio.NewReader(events)
	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return s.report, readErr
		}
		if line != "" {
			if err := s.line(line); err != nil {
				return s.report, err
			}
		}
		if readErr != nil {
			return s.report, nil
		}
	}
}

func (s *stream) line(line string) error {
	var record event
	if !strings.HasPrefix(line, "{") || json.Unmarshal([]byte(line), &record) != nil {
		if !strings.HasSuffix(line, "\n") {
			line += "\n"
		}
		return s.write(line)
	}
	key := test{record.Package, record.Test}
	switch record.Action {
	case "build-output":
		buffer(s.builds, record.ImportPath).WriteString(record.Output)
	case "build-fail":
		text := buffer(s.builds, record.ImportPath).String()
		delete(s.builds, record.ImportPath)
		return s.write(text)
	case "output":
		if _, ok := s.held[key]; !ok && record.Test != "" {
			s.started[record.Package] = append(s.started[record.Package], record.Test)
		}
		buffer(s.held, key).WriteString(record.Output)
	case "pass", "skip", "fail":
		if record.Test == "" {
			return s.endPackage(record.Package, record.Action == "fail")
		}
		text := buffer(s.held, key).String()
		delete(s.held, key)
		switch record.Action {
		case "pass":
			s.report.passed++
		case "skip":
			s.report.skipped++
		default:
			s.report.failed++
			return s.write(text)
		}
	}
	return nil
}

func (s *stream) endPackage(pkg string, failed bool) error {
	s.report.packages++
	var text strings.Builder
	if failed {
		s.report.failedPackages++
		// A timeout or crash ends the package while tests are still running.
		for _, name := range s.started[pkg] {
			if output, ok := s.held[test{pkg, name}]; ok {
				text.WriteString(output.String())
			}
		}
		if output, ok := s.held[test{pkg, ""}]; ok {
			text.WriteString(output.String())
		}
	}
	for _, name := range s.started[pkg] {
		delete(s.held, test{pkg, name})
	}
	delete(s.held, test{pkg, ""})
	delete(s.started, pkg)
	return s.write(text.String())
}

func (s *stream) write(text string) error {
	_, err := io.WriteString(s.output, text)
	return err
}

func buffer[K comparable](buffers map[K]*strings.Builder, key K) *strings.Builder {
	if buffers[key] == nil {
		buffers[key] = &strings.Builder{}
	}
	return buffers[key]
}

// summary is the one line written after the stream, given go test's exit
// code.
func (r report) summary(code int) string {
	if code == 0 && r.failed == 0 && r.failedPackages == 0 {
		return fmt.Sprintf("ok: %s passed, %d skipped in %s",
			count(r.passed, "test"), r.skipped, count(r.packages, "package"))
	}
	return fmt.Sprintf("FAIL: %s failed, %d passed, %d skipped; %d of %s failed",
		count(r.failed, "test"), r.passed, r.skipped, r.failedPackages, count(r.packages, "package"))
}

func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
