package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
)

func readSelectionFile(path string) (controlSelection, error) {
	var s controlSelection
	file, err := os.Open(path)
	if err != nil {
		return s, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return s, err
	}
	if len(data) > 4096 {
		return s, errors.New("selection file exceeds4096bytes")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&s); err != nil {
		return s, err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return s, errors.New("exactly one selection object required")
	}
	_, err = parseSelection(s)
	return s, err
}
