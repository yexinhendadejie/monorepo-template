// Package recipe reads machine files without changing their measurement scale.
package recipe

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

type Field struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Parameter struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Value       string   `json:"value"`
	Unit        string   `json:"unit"`
	Description string   `json:"description"`
	Raw         []string `json:"raw"`
}

type Test struct {
	ID          string      `json:"id"`
	Order       int         `json:"order"`
	Number      string      `json:"number"`
	Name        string      `json:"name"`
	Alias       string      `json:"alias"`
	Kind        string      `json:"kind"`
	Function    string      `json:"function"`
	Lower       string      `json:"lower"`
	Upper       string      `json:"upper"`
	Unit        string      `json:"unit"`
	Time        string      `json:"time"`
	Bias        string      `json:"bias"`
	Description string      `json:"description"`
	SourceLine  int         `json:"sourceLine"`
	Fields      []Field     `json:"fields"`
	Parameters  []Parameter `json:"parameters"`
}

type Body struct {
	MachineID  string   `json:"machineId"`
	SourceName string   `json:"sourceName"`
	Format     string   `json:"format"`
	Encoding   string   `json:"encoding"`
	SHA256     string   `json:"sha256"`
	Tests      []Test   `json:"tests"`
	Metadata   []Field  `json:"metadata"`
	Warnings   []string `json:"warnings"`
}

const MaxFileBytes = 10 << 20

func Parse(machineID, filename string, data []byte) (*Body, error) {
	if len(data) == 0 || len(data) > MaxFileBytes {
		return nil, fmt.Errorf("empty or oversized recipe")
	}
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	if format != "pgs" && format != "dcex" {
		return nil, fmt.Errorf("unsupported recipe format")
	}
	encoding := "UTF-8"
	text := data
	if !utf8.Valid(data) {
		var err error
		text, err = simplifiedchinese.GBK.NewDecoder().Bytes(data)
		if err != nil || bytes.Contains(text, []byte("\uFFFD")) {
			return nil, fmt.Errorf("invalid GBK recipe")
		}
		encoding = "GBK"
	}
	body := &Body{MachineID: machineID, SourceName: filename, Format: format, Encoding: encoding,
		SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Tests: []Test{}, Metadata: []Field{}, Warnings: []string{}}
	lines := strings.Split(strings.TrimPrefix(string(text), "\uFEFF"), "\n")
	var err error
	if format == "pgs" {
		err = parsePGS(body, lines)
	} else {
		err = parseDCEX(body, lines)
	}
	if err != nil {
		return nil, err
	}
	if len(body.Tests) == 0 {
		return nil, fmt.Errorf("recipe has no test items")
	}
	return body, nil
}

func csvFields(line string, count int) ([]string, error) {
	reader := csv.NewReader(strings.NewReader(strings.TrimSpace(line)))
	reader.FieldsPerRecord = count
	return reader.Read()
}

func parsePGS(body *Body, lines []string) error {
	active, function, functionLine := false, "", 0
	expected, actual, expectedParams := 0, 0, 0
	functions, declared := 0, -1
	check := func() error {
		if function != "" && actual != expected {
			return fmt.Errorf("line %d: function item count mismatch", functionLine)
		}
		return nil
	}
	checkParams := func() error {
		if len(body.Tests) > 0 && len(body.Tests[len(body.Tests)-1].Parameters) != expectedParams {
			return fmt.Errorf("parameter count mismatch")
		}
		return nil
	}
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if active {
				if err := check(); err != nil {
					return err
				}
				if err := checkParams(); err != nil {
					return err
				}
			}
			active = line == "[DataSheet Variable Block]"
			function = ""
			continue
		}
		if active && strings.HasPrefix(line, "FUNCTION =") {
			if err := check(); err != nil {
				return err
			}
			if err := checkParams(); err != nil {
				return err
			}
			fields, err := csvFields(strings.TrimSpace(strings.TrimPrefix(line, "FUNCTION =")), 3)
			if err != nil {
				return fmt.Errorf("line %d: invalid function: %w", i+1, err)
			}
			expected, err = strconv.Atoi(fields[2])
			if err != nil || expected < 1 {
				return fmt.Errorf("line %d: invalid function item count", i+1)
			}
			function, actual, functionLine = fields[0], 0, i+1
			functions++
			continue
		}
		if active && strings.HasPrefix(raw, "        ") {
			if function == "" || actual == 0 {
				return fmt.Errorf("line %d: orphan parameter", i+1)
			}
			fields, err := csvFields(line, 9)
			if err != nil {
				return fmt.Errorf("line %d: invalid parameter: %w", i+1, err)
			}
			test := &body.Tests[len(body.Tests)-1]
			test.Parameters = append(test.Parameters, Parameter{fields[0], fields[1], fields[6], fields[7], fields[8], fields})
			display := strings.TrimSpace(fields[6] + " " + fields[7])
			if fields[0] == "ForceTime" || fields[0] == "PulseTime" {
				test.Time = display
			}
			switch fields[0] {
			case "Vgs", "Vds", "Id", "Is", "Ik":
				if test.Bias != "" {
					test.Bias += "; "
				}
				test.Bias += fields[0] + " = " + display
			}
			continue
		}
		if active && strings.HasPrefix(raw, "    ") {
			if function == "" {
				return fmt.Errorf("line %d: orphan test", i+1)
			}
			if err := checkParams(); err != nil {
				return err
			}
			fields, err := csvFields(line, 12)
			if err != nil {
				return fmt.Errorf("line %d: invalid test: %w", i+1, err)
			}
			expectedParams, err = strconv.Atoi(fields[10])
			if err != nil || expectedParams < 0 {
				return fmt.Errorf("line %d: invalid parameter count", i+1)
			}
			kind := "measurement"
			if function == "VIRTUALFUNC" || function == "RELAY_INIT" || strings.HasSuffix(function, "_CTRL") {
				kind = "control"
			}
			names := []string{"Type", "Alias", "Flag1", "Flag2", "Lower", "Upper", "UnitOptions", "Unit", "DisplayFormat", "Flag3", "ParameterCount", "Description"}
			test := Test{ID: fmt.Sprintf("pgs-L%d", i+1), Order: len(body.Tests) + 1, Name: fields[0], Alias: fields[1], Kind: kind,
				Function: function, Lower: fields[4], Upper: fields[5], Unit: fields[7], Description: fields[11], SourceLine: i + 1, Fields: []Field{}, Parameters: []Parameter{}}
			for j, name := range names {
				test.Fields = append(test.Fields, Field{name, fields[j]})
			}
			body.Tests = append(body.Tests, test)
			actual++
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok {
			key, value = strings.TrimSpace(key), strings.TrimSpace(value)
			body.Metadata = append(body.Metadata, Field{key, value})
			if active && key == "TestFunctionCount" {
				var err error
				declared, err = strconv.Atoi(value)
				if err != nil {
					return fmt.Errorf("invalid TestFunctionCount")
				}
			}
		} else if active {
			return fmt.Errorf("line %d: unrecognized data line", i+1)
		}
	}
	if err := check(); err != nil {
		return err
	}
	if err := checkParams(); err != nil {
		return err
	}
	if declared < 0 || declared != functions {
		return fmt.Errorf("function count mismatch: declared %d, parsed %d", declared, functions)
	}
	body.Warnings = append(body.Warnings, "PGS 标志位按原值保留；控制步骤单独标记，未推断启用或判定语义。")
	return nil
}

func parseDCEX(body *Body, lines []string) error {
	section, start, declared := "", 0, -1
	fields := []Field{}
	seen := map[string]bool{}
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[/") {
			if section == "" || line != "[/"+section+"]" {
				return fmt.Errorf("line %d: mismatched section close", i+1)
			}
			if strings.HasPrefix(section, "TEST PLAN ") {
				get := func(key string) string {
					for _, field := range fields {
						if field.Key == key {
							return field.Value
						}
					}
					return ""
				}
				if get("Name") == "" {
					return fmt.Errorf("line %d: missing test name", start)
				}
				body.Tests = append(body.Tests, Test{ID: fmt.Sprintf("dcex-L%d", start), Order: len(body.Tests) + 1,
					Number: get("Test No"), Name: get("Name"), Alias: get("Alias"), Kind: "measurement", Function: get("Code"),
					Lower: get("Lmt Lo"), Upper: get("Lmt Hi"), Unit: "编码未确认", Time: get("Time") + "（Range=" + get("Time Range") + "）",
					Bias: "原始 Bias 编码，见详情", SourceLine: start, Fields: fields, Parameters: []Parameter{}})
			} else {
				for _, field := range fields {
					body.Metadata = append(body.Metadata, Field{section + " / " + field.Key, field.Value})
					if section == "INFO PLAN" && field.Key == "Test Plan Cnt" {
						var err error
						declared, err = strconv.Atoi(field.Value)
						if err != nil {
							return fmt.Errorf("invalid Test Plan Cnt")
						}
					}
				}
			}
			section = ""
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if section != "" {
				return fmt.Errorf("line %d: unclosed section", i+1)
			}
			section = strings.Trim(line, "[]")
			if seen[section] {
				return fmt.Errorf("line %d: duplicate section", i+1)
			}
			seen[section] = true
			start, fields = i+1, []Field{}
			continue
		}
		if section == "" || !strings.HasPrefix(line, "<") {
			return fmt.Errorf("line %d: invalid DCEX field", i+1)
		}
		key, value, ok := strings.Cut(strings.TrimPrefix(line, "<"), ">")
		if !ok || key == "" {
			return fmt.Errorf("line %d: invalid DCEX key", i+1)
		}
		for _, field := range fields {
			if field.Key == key {
				return fmt.Errorf("line %d: duplicate DCEX key", i+1)
			}
		}
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "\"") {
			var err error
			value, err = strconv.Unquote(value)
			if err != nil {
				return fmt.Errorf("line %d: invalid quoted value", i+1)
			}
		}
		fields = append(fields, Field{key, value})
	}
	if section != "" {
		return fmt.Errorf("unclosed DCEX section")
	}
	if declared < 0 || declared != len(body.Tests) {
		return fmt.Errorf("test count mismatch: declared %d, parsed %d", declared, len(body.Tests))
	}
	body.Warnings = append(body.Warnings, "DCEX 的 Unit、Range 与 Bias 编码尚待厂商确认；上下限为原始数值，不代表已换算物理量。")
	return nil
}
