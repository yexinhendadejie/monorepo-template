package recipe

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const pgs = `[Document Block]
strDutName = Demo
[DataSheet Variable Block]
TestFunctionCount = 1
FUNCTION = TEST_IGSS,1,2
    IGSS,IGSS1,1,1,-25,2,"mA;uA;nA",nA,"0.0000",1,2,"Leakage, test"
        Vgs,Vgs,0,0,"-5","V","-6","V","Gate voltage"
        ForceTime,ForceTime,0,0,"1","ms;us","30","ms","Forcing time"
    IGSS,IGSS1,0,0,,,"nA",nA,"0.0000",1,0,""
[Station Setting Block]
Site = 1
`

const dcex = `[INFO PLAN]
 <Test Plan Cnt>1
 <Update>"2026/10/30"
[/INFO PLAN]
[TEST PLAN 1]
 <Name>"KEL_C"
 <Alias>"Kelvin"
 <Test No>"T1"
 <Code>41267
 <Test En>0
 <Lmt Lo>2.5
 <Lmt Hi>3.5
 <Lmt Lo Unit>0
 <Lmt Hi Unit>0
 <Lmt Lo Range>10
 <Bias2>10000000000
 <Time>3
 <Time Range>6
[/TEST PLAN 1]
`

func mustParse(t *testing.T, filename, source string) *Body {
	t.Helper()
	body, err := Parse("M1", filename, []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestPGSUsesCurrentParameterValuesAndPreservesDuplicateItems(t *testing.T) {
	body := mustParse(t, "sample.PGS", pgs)
	if len(body.Tests) != 2 {
		t.Fatalf("got %d items", len(body.Tests))
	}
	first, second := body.Tests[0], body.Tests[1]
	if body.Format != "pgs" || body.MachineID != "M1" || body.Encoding != "UTF-8" {
		t.Fatalf("unexpected source: %+v", body)
	}
	if body.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(pgs))) {
		t.Fatal("digest must refer to original bytes")
	}
	if first.Name != "IGSS" || first.Alias != "IGSS1" || first.Lower != "-25" || first.Upper != "2" || first.Unit != "nA" {
		t.Fatalf("wrong test fields: %+v", first)
	}
	if first.Time != "30 ms" || first.Bias != "Vgs = -6 V" {
		t.Fatalf("defaults used instead of current values: %+v", first)
	}
	if first.Description != "Leakage, test" {
		t.Fatal("quoted CSV comma lost")
	}
	if first.Parameters[0].Raw[4] != "-5" || first.Parameters[0].Value != "-6" {
		t.Fatal("raw parameter options and current values must coexist")
	}
	if first.ID == second.ID || second.Order != 2 || first.SourceLine != 6 {
		t.Fatal("repeated names must retain distinct source identities and order")
	}
	if second.Lower != "" || second.Upper != "" {
		t.Fatal("missing limits must remain absent, not zero")
	}
	if len(body.Metadata) != 3 || len(body.Warnings) != 1 {
		t.Fatalf("missing metadata or warnings: %+v", body)
	}
}

func TestPGSControlClassificationAndMultipleBiases(t *testing.T) {
	for _, function := range []string{"VIRTUALFUNC", "RELAY_INIT", "TEST_BOX_SELECT_CTRL", "TEST_IGSS"} {
		t.Run(function, func(t *testing.T) {
			source := strings.ReplaceAll(pgs, "TEST_IGSS", function)
			body := mustParse(t, "demo.pgs", source)
			want := "control"
			if function == "TEST_IGSS" {
				want = "measurement"
			}
			if body.Tests[0].Kind != want {
				t.Fatalf("got %q", body.Tests[0].Kind)
			}
		})
	}
	source := strings.Replace(pgs, ",1,2,\"Leakage", ",1,3,\"Leakage", 1)
	source = strings.Replace(source, "    IGSS,IGSS1,0", "        Id,Id,0,0,0,mA,2,mA,current\n    IGSS,IGSS1,0", 1)
	body := mustParse(t, "demo.pgs", source)
	if body.Tests[0].Bias != "Vgs = -6 V; Id = 2 mA" {
		t.Fatal(body.Tests[0].Bias)
	}
	pulse := strings.Replace(pgs, "ForceTime", "PulseTime", 1)
	if mustParse(t, "demo.pgs", pulse).Tests[0].Time != "30 ms" {
		t.Fatal("pulse time missing")
	}
}

func TestDCEXPreservesUnknownCodesAndSentinels(t *testing.T) {
	body := mustParse(t, "demo.dcex", "\uFEFF"+strings.ReplaceAll(dcex, "\n", "\r\n"))
	test := body.Tests[0]
	if test.Name != "KEL_C" || test.Alias != "Kelvin" || test.Number != "T1" {
		t.Fatalf("wrong identity: %+v", test)
	}
	if test.Lower != "2.5" || test.Upper != "3.5" || test.Unit != "编码未确认" || test.Time != "3（Range=6）" {
		t.Fatalf("incorrectly converted: %+v", test)
	}
	if test.SourceLine != 5 || test.ID != "dcex-L5" {
		t.Fatal("source line changed")
	}
	values := map[string]string{}
	for _, field := range test.Fields {
		values[field.Key] = field.Value
	}
	if values["Bias2"] != "10000000000" || values["Lmt Lo Unit"] != "0" || values["Test En"] != "0" {
		t.Fatal("unknown codes lost")
	}
	if len(body.Metadata) != 2 || len(body.Warnings) != 1 {
		t.Fatal("metadata and uncertainty must be visible")
	}
}

func TestLegacyGBKAndOriginalDigest(t *testing.T) {
	input, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(strings.Replace(pgs, "Leakage, test", "漏电流测试", 1)))
	if err != nil {
		t.Fatal(err)
	}
	body, err := Parse("M1", "demo.pgs", input)
	if err != nil {
		t.Fatal(err)
	}
	if body.Encoding != "GBK" || body.Tests[0].Description != "漏电流测试" {
		t.Fatal("GBK decoding failed")
	}
	if body.SHA256 != fmt.Sprintf("%x", sha256.Sum256(input)) {
		t.Fatal("digest must not use UTF-8 transcoding")
	}
}

func TestInvalidFilesFailInsteadOfProducingPartialRecipes(t *testing.T) {
	cases := []struct{ name, filename, source, message string }{
		{"empty", "a.pgs", "", "empty"},
		{"unsupported", "a.exe", "content", "unsupported"},
		{"no items", "a.pgs", "[DataSheet Variable Block]\nTestFunctionCount = 0", "no test items"},
		{"missing function count", "a.pgs", strings.Replace(pgs, "TestFunctionCount = 1", "", 1), "function count mismatch"},
		{"bad function count", "a.pgs", strings.Replace(pgs, "TestFunctionCount = 1", "TestFunctionCount = x", 1), "invalid TestFunctionCount"},
		{"function count mismatch", "a.pgs", strings.Replace(pgs, "TestFunctionCount = 1", "TestFunctionCount = 2", 1), "function count mismatch"},
		{"bad function", "a.pgs", strings.Replace(pgs, "TEST_IGSS,1,2", "TEST_IGSS,1", 1), "invalid function"},
		{"bad item count", "a.pgs", strings.Replace(pgs, "TEST_IGSS,1,2", "TEST_IGSS,1,x", 1), "invalid function item count"},
		{"zero item count", "a.pgs", strings.Replace(pgs, "TEST_IGSS,1,2", "TEST_IGSS,1,0", 1), "invalid function item count"},
		{"item mismatch", "a.pgs", strings.Replace(pgs, "TEST_IGSS,1,2", "TEST_IGSS,1,3", 1), "function item count mismatch"},
		{"parameter mismatch", "a.pgs", strings.Replace(pgs, ",1,2,\"Leakage", ",1,3,\"Leakage", 1), "parameter count mismatch"},
		{"bad test CSV", "a.pgs", strings.Replace(pgs, "IGSS,IGSS1,1,1,-25", "IGSS,1,-25", 1), "invalid test"},
		{"bad parameter CSV", "a.pgs", strings.Replace(pgs, "Vgs,Vgs,0,0", "Vgs,0", 1), "invalid parameter"},
		{"bad param count", "a.pgs", strings.Replace(pgs, ",1,2,\"Leakage", ",1,x,\"Leakage", 1), "invalid parameter count"},
		{"orphan parameter", "a.pgs", strings.Replace(pgs, "FUNCTION = TEST_IGSS,1,2", "        Vgs,Vgs,0,0,0,V,0,V,test", 1), "orphan parameter"},
		{"orphan test", "a.pgs", strings.Replace(pgs, "FUNCTION = TEST_IGSS,1,2", "", 1), "orphan test"},
		{"unknown data", "a.pgs", strings.Replace(pgs, "TestFunctionCount = 1", "unsupported syntax", 1), "unrecognized data line"},
		{"bad GBK", "a.pgs", "\xff", "invalid GBK"},
		{"missing name", "a.dcex", strings.Replace(dcex, "<Name>\"KEL_C\"", "<Name>\"\"", 1), "missing test name"},
		{"count mismatch", "a.dcex", strings.Replace(dcex, "Cnt>1", "Cnt>2", 1), "test count mismatch"},
		{"missing count", "a.dcex", strings.Replace(dcex, "<Test Plan Cnt>1", "", 1), "test count mismatch"},
		{"invalid count", "a.dcex", strings.Replace(dcex, "Cnt>1", "Cnt>x", 1), "invalid Test Plan Cnt"},
		{"closing section", "a.dcex", strings.Replace(dcex, "[/TEST PLAN 1]", "[/TEST PLAN 2]", 1), "mismatched section close"},
		{"duplicate section", "a.dcex", dcex + dcex, "duplicate section"},
		{"unclosed", "a.dcex", strings.Replace(dcex, "[/TEST PLAN 1]", "", 1), "unclosed DCEX section"},
		{"nested", "a.dcex", strings.Replace(dcex, "[/INFO PLAN]", "", 1), "unclosed section"},
		{"invalid field", "a.dcex", strings.Replace(dcex, "<Code>41267", "Code=41267", 1), "invalid DCEX field"},
		{"invalid key", "a.dcex", strings.Replace(dcex, "<Code>41267", "<>41267", 1), "invalid DCEX key"},
		{"missing key end", "a.dcex", strings.Replace(dcex, "<Code>41267", "<Code41267", 1), "invalid DCEX key"},
		{"duplicate field", "a.dcex", strings.Replace(dcex, "<Code>41267", "<Code>41267\n<Code>1", 1), "duplicate DCEX key"},
		{"bad quotes", "a.dcex", strings.Replace(dcex, "<Name>\"KEL_C\"", "<Name>\"KEL_C", 1), "invalid quoted value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := Parse("M1", tc.filename, []byte(tc.source))
			if err == nil || !strings.Contains(err.Error(), tc.message) || body != nil {
				t.Fatalf("wanted %q, got body=%v error=%v", tc.message, body, err)
			}
		})
	}
	_, err := Parse("M1", "a.pgs", bytes.Repeat([]byte("x"), MaxFileBytes+1))
	if err == nil {
		t.Fatal("oversized input accepted")
	}
}

// 真实机台数据不入库，先 make sync 再跑这个巡检；没有数据时跳过而不是失败。
func TestEverySyncedMachineFile(t *testing.T) {
	paths, err := filepath.Glob("../../machine-data/*/*")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Skip("machine-data 为空，跳过真实机台数据巡检（先 make sync）")
	}
	for _, path := range paths {
		t.Run(filepath.Base(filepath.Dir(path))+"/"+filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			body, err := Parse(filepath.Base(filepath.Dir(path)), filepath.Base(path), data)
			if err != nil {
				t.Fatal(err)
			}
			if len(body.Tests) == 0 {
				t.Fatal("no items parsed")
			}
			ids := map[string]bool{}
			for i, test := range body.Tests {
				if ids[test.ID] || test.Order != i+1 || test.Name == "" || len(test.Fields) == 0 {
					t.Fatalf("lost source identity: %+v", test)
				}
				ids[test.ID] = true
			}
			if filepath.Base(path) == "NP3D-1200-2920-6890-A1_8.dcex" && len(body.Tests) != 14 {
				t.Fatalf("DCEX fixture count mismatch: %d", len(body.Tests))
			}
			if strings.Contains(path, "NP4M-1500-2800-4750-A1_8.pgs") {
				if len(body.Tests) != 28 {
					t.Fatalf("26 functions should produce 28 items, got %d", len(body.Tests))
				}
				for _, test := range body.Tests {
					if test.Alias == "IGSS1" && (test.Lower != "-25" || test.Upper != "2" || test.Time != "30 ms") {
						t.Fatalf("source regression: %+v", test)
					}
				}
			}
		})
	}
}
