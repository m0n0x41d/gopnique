package debugfiles

import (
	"bufio"
	"bytes"
	"errors"
	"strconv"
	"strings"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

type breakpadSymbols struct {
	debugID   domain.DebugIdentifier
	codeFile  string
	functions []breakpadFunction
}

type breakpadFunction struct {
	address uint64
	size    uint64
	name    string
}

func parseBreakpadSymbols(payload []byte) (breakpadSymbols, error) {
	scanner := bufio.NewScanner(bytes.NewReader(payload))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	symbols := breakpadSymbols{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "MODULE ") {
			parsed, parseErr := parseBreakpadModule(line)
			if parseErr != nil {
				return breakpadSymbols{}, parseErr
			}

			symbols.debugID = parsed.debugID
			symbols.codeFile = parsed.codeFile
			continue
		}

		if strings.HasPrefix(line, "FUNC ") {
			function, ok := parseBreakpadFunction(line)
			if !ok {
				continue
			}

			symbols.functions = append(symbols.functions, function)
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return breakpadSymbols{}, scanErr
	}

	if symbols.debugID.String() == "" {
		return breakpadSymbols{}, errors.New("breakpad symbols require module header")
	}

	return symbols, nil
}

func parseBreakpadModule(line string) (breakpadSymbols, error) {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return breakpadSymbols{}, errors.New("breakpad module header is invalid")
	}

	debugID, debugIDErr := domain.NewDebugIdentifier(fields[3])
	if debugIDErr != nil {
		return breakpadSymbols{}, debugIDErr
	}

	return breakpadSymbols{
		debugID:  debugID,
		codeFile: strings.Join(fields[4:], " "),
	}, nil
}

func parseBreakpadFunction(line string) (breakpadFunction, bool) {
	fields := strings.Fields(line)
	offset := 1
	if len(fields) > 1 && fields[1] == "m" {
		offset = 2
	}

	if len(fields) < offset+4 {
		return breakpadFunction{}, false
	}

	address, addressErr := parseBreakpadHex(fields[offset])
	if addressErr != nil {
		return breakpadFunction{}, false
	}

	size, sizeErr := parseBreakpadHex(fields[offset+1])
	if sizeErr != nil {
		return breakpadFunction{}, false
	}

	name := strings.TrimSpace(strings.Join(fields[offset+3:], " "))
	if name == "" {
		return breakpadFunction{}, false
	}

	return breakpadFunction{
		address: address,
		size:    size,
		name:    name,
	}, true
}

func parseBreakpadHex(input string) (uint64, error) {
	value := strings.TrimSpace(input)
	value = strings.TrimPrefix(value, "0x")
	value = strings.TrimPrefix(value, "0X")

	return strconv.ParseUint(value, 16, 64)
}

func (symbols breakpadSymbols) lookup(offset uint64) (string, bool) {
	for _, function := range symbols.functions {
		if !function.contains(offset) {
			continue
		}

		return function.name, true
	}

	return "", false
}

func (function breakpadFunction) contains(offset uint64) bool {
	if offset < function.address {
		return false
	}

	if function.size == 0 {
		return offset == function.address
	}

	return offset-function.address < function.size
}
