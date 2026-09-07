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

func TestExecEMFWithErrorReturnsFailures(t *testing.T) {
	if err := ExecEMFWithError([]byte{1, 2, 3}, ""); err == nil {
		t.Fatal("invalid EMF should return an error")
	}
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

func TestCheckUnhandledRecords(t *testing.T) {
	file, err := os.ReadFile("image11.emf")
	if err != nil {
		t.Fatal(err)
	}
	emfFile, err := ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	unhandled := make(map[uint32]int)
	for _, rec := range emfFile.Records {
		val := reflect.ValueOf(rec)
		if val.Kind() == reflect.Ptr {
			val = val.Elem()
		}
		var rType uint32
		f := val.FieldByName("Record")
		if f.IsValid() {
			rType = uint32(f.FieldByName("Type").Uint())
		} else {
			fType := val.FieldByName("Type")
			if fType.IsValid() {
				rType = uint32(fType.Uint())
			}
		}

		fn, exists := records[rType]
		if !exists || fn == nil {
			unhandled[rType]++
		}
	}
	t.Logf("Unhandled/nil record types in image11.emf:")
	for rType, count := range unhandled {
		t.Logf("  Type 0x%02x (%d): %d", rType, rType, count)
	}
}
