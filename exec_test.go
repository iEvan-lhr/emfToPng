package emf

import (
	"log"
	"os"
	"reflect"
	"testing"
)

func TestExecEMF(t *testing.T) {
	file, err := os.ReadFile("image1.emf")
	if err != nil {
		log.Fatal(err)
	}
	ExecEMF(file, "image1.emf")

	file2, err := os.ReadFile("image11.emf")
	if err != nil {
		log.Fatal(err)
	}
	ExecEMF(file2, "image11.emf")
}

func TestAnalyzeEMF(t *testing.T) {
	file, err := os.ReadFile("image11.emf")
	if err != nil {
		t.Fatal(err)
	}
	emfFile, err := ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Header bounds: %+v", emfFile.Header.Bounds)
	t.Logf("Number of records: %d", len(emfFile.Records))
	counts := make(map[uint32]int)
	for _, rec := range emfFile.Records {
		v := reflect.ValueOf(rec)
		if v.Kind() == reflect.Ptr {
			v = v.Elem()
		}
		var rType uint32
		f := v.FieldByName("Record")
		if f.IsValid() {
			rType = uint32(f.FieldByName("Type").Uint())
		} else {
			fType := v.FieldByName("Type")
			if fType.IsValid() {
				rType = uint32(fType.Uint())
			}
		}
		counts[rType]++
	}
	t.Logf("Record type counts:")
	for rType, count := range counts {
		t.Logf("  Type 0x%x (%d): %d", rType, rType, count)
	}
}
