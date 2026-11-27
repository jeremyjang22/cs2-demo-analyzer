// Package envfile loads KEY=value lines from a .env file into the process
// environment, for commands run on a developer's machine.
//
// A variable already set wins over the file, so the same binary reads its
// settings from the service environment on Northflank and from
// packages/api/.env on the PC without a flag telling it which.
//
// Parsed line by line, never by a shell: the connection strings hold `&`
// (?search_path=public&sslmode=require), which `source` reads as "run this in
// the background", leaving the variable silently empty.
package envfile

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"strings"
)

// Load sets each variable in path that is not already set. A missing file is
// not an error - on a server there is none - and reports false.
func Load(path string) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, value, ok := parseLine(sc.Text())
		if !ok {
			continue
		}
		if _, set := os.LookupEnv(key); set {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return true, err
		}
	}
	return true, sc.Err()
}

// parseLine reads KEY=value, allowing `export `, surrounding quotes, and the
// \r a Windows editor leaves on every line.
func parseLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")
	key, value, ok = strings.Cut(line, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" || strings.ContainsAny(key, " \t") {
		return "", "", false
	}
	value = strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		value = value[1 : len(value)-1]
	}
	return key, value, true
}
