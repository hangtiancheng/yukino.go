package bridge

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

func ReadSSE(r io.Reader, handle func(string, string) (bool, error)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var event string
	var data []string
	dispatch := func() (bool, error) {
		if len(data) == 0 {
			event = ""
			return true, nil
		}
		more, err := handle(event, strings.Join(data, "\n"))
		event = ""
		data = nil
		return more, err
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			more, err := dispatch()
			if err != nil || !more {
				return err
			}
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			data = append(data, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read upstream stream: %w", err)
	}
	_, err := dispatch()
	return err
}

type Collector struct{ Response Object }

func (c *Collector) Sink(event string, data Object) error {
	switch event {
	case "response.completed", "response.incomplete":
		c.Response = Obj(data["response"])
	case "response.failed", "error":
		return fmt.Errorf("upstream stream failed")
	}
	return nil
}
func (c *Collector) Result() (Object, error) {
	if c.Response == nil {
		return nil, fmt.Errorf("stream ended before a terminal response")
	}
	return c.Response, nil
}
